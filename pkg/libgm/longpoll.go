package libgm

import (
	"bufio"
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"sync"
	"sync/atomic"
	"time"

	"github.com/google/uuid"
	"github.com/rs/zerolog"
	"go.mau.fi/util/exhttp"
	"go.mau.fi/util/pblite"
	"google.golang.org/protobuf/proto"

	"go.mau.fi/mautrix-gmessages/pkg/libgm/events"
	"go.mau.fi/mautrix-gmessages/pkg/libgm/gmproto"
	"go.mau.fi/mautrix-gmessages/pkg/libgm/util"
)

const defaultPingTimeout = 1 * time.Minute
const shortPingTimeout = 10 * time.Second
const minPingInterval = 30 * time.Second
const maxRepingTickerTime = 64 * time.Minute

// receiveIdleTimeout bounds how long the foreground ReceiveMessages long-poll may
// go without any frame before it is treated as dead and reconnected. A healthy
// stream emits a server heartbeat every ~10s (measured), so this is 3 missed
// heartbeats — long enough to avoid false positives, short enough to recover a
// silent stall quickly.
const receiveIdleTimeout = 30 * time.Second

var pingIDCounter atomic.Uint64

// Goals of the ditto pinger:
//   - By default, send pings to the phone every minute
//   - If an outgoing request doesn't respond quickly, send a ping immediately
//   - If a ping caused by a request timeout doesn't respond quickly, send PhoneNotResponding
//     (the user is probably actively trying to use the bridge)
//   - If the first ping doesn't respond, send PhoneNotResponding
//     (to avoid the bridge being stuck in the CONNECTING state)
//   - If a ping doesn't respond, send new pings on increasing intervals
//     (starting from 1 minute up to 1 hour) until it responds
//   - If a normal ping doesn't respond, send PhoneNotResponding after 3 failed pings
//     (so after ~8 minutes in total, not faster to avoid unnecessarily spamming the user)
//   - If a request timeout happens during backoff pings, send PhoneNotResponding immediately
//   - If a ping responds and PhoneNotResponding was sent, send PhoneRespondingAgain
type dittoPinger struct {
	client *Client

	firstPingDone     bool
	pingHandlingLock  sync.RWMutex
	oldestPingTime    time.Time
	lastPingTime      time.Time
	pingFails         int
	notRespondingSent bool
	pingInterval      time.Duration
	alertTimeoutCount int

	stop <-chan struct{}
	log  *zerolog.Logger
}

type resetter struct {
	C chan struct{}
	d atomic.Bool
}

func newResetter() *resetter {
	return &resetter{
		C: make(chan struct{}),
	}
}

func (r *resetter) Done() {
	if r.d.CompareAndSwap(false, true) {
		go func() {
			time.Sleep(5 * time.Second)
			close(r.C)
		}()
	}
}

func (dp *dittoPinger) OnRespond(pingID uint64, dur time.Duration, reset *resetter) {
	dp.pingHandlingLock.Lock()
	defer dp.pingHandlingLock.Unlock()
	logEvt := dp.log.Debug().Uint64("ping_id", pingID).Dur("duration", dur)
	if dp.notRespondingSent {
		logEvt.Msg("Ditto ping successful (phone is back online)")
		dp.client.triggerEvent(&events.PhoneRespondingAgain{})
	} else if dp.pingFails > 0 {
		logEvt.Msg("Ditto ping successful (stopped failing)")
		// TODO separate event?
		dp.client.triggerEvent(&events.PhoneRespondingAgain{})
	} else {
		logEvt.Msg("Ditto ping successful")
	}
	dp.oldestPingTime = time.Time{}
	dp.notRespondingSent = false
	dp.pingFails = 0
	dp.firstPingDone = true
	reset.Done()
}

func (dp *dittoPinger) OnTimeout(pingID uint64, sendNotResponding bool) {
	dp.pingHandlingLock.Lock()
	defer dp.pingHandlingLock.Unlock()
	dp.log.Warn().Uint64("ping_id", pingID).Msg("Ditto ping is taking long, phone may be offline")
	if (!dp.firstPingDone || sendNotResponding) && !dp.notRespondingSent {
		dp.client.triggerEvent(&events.PhoneNotResponding{})
		dp.notRespondingSent = true
	}
}

func (dp *dittoPinger) WaitForResponse(pingID uint64, start time.Time, timeout time.Duration, timeoutCount int, pingChan <-chan *IncomingRPCMessage, reset *resetter) {
	var timerChan <-chan time.Time
	var timer *time.Timer
	if timeout > 0 {
		timer = time.NewTimer(timeout)
		timerChan = timer.C
	}
	select {
	case <-pingChan:
		dp.OnRespond(pingID, time.Since(start), reset)
		if timer != nil && !timer.Stop() {
			<-timer.C
		}
	case <-timerChan:
		dp.OnTimeout(pingID, timeout == shortPingTimeout || timeoutCount >= dp.alertTimeoutCount)
		repingTickerTime := 1 * time.Minute
		var repingTicker *time.Ticker
		var repingTickerChan <-chan time.Time
		if timeoutCount == 0 {
			repingTicker = time.NewTicker(repingTickerTime)
			repingTickerChan = repingTicker.C
		}
		for {
			timeoutCount++
			select {
			case <-pingChan:
				dp.OnRespond(pingID, time.Since(start), reset)
				return
			case <-repingTickerChan:
				if repingTickerTime < maxRepingTickerTime {
					repingTickerTime *= 2
					repingTicker.Reset(repingTickerTime)
				}
				subPingID := pingIDCounter.Add(1)
				dp.log.Debug().
					Uint64("parent_ping_id", pingID).
					Uint64("ping_id", subPingID).
					Str("next_reping", repingTickerTime.String()).
					Msg("Sending new ping")
				dp.Ping(subPingID, defaultPingTimeout, timeoutCount, reset)
			case <-dp.client.pingShortCircuit:
				dp.pingHandlingLock.Lock()
				dp.log.Debug().Uint64("ping_id", pingID).
					Msg("Ditto ping wait short-circuited during ping backoff, sending PhoneNotResponding immediately")
				if !dp.notRespondingSent {
					dp.client.triggerEvent(&events.PhoneNotResponding{})
					dp.notRespondingSent = true
				}
				dp.pingHandlingLock.Unlock()
			case <-dp.stop:
				return
			case <-reset.C:
				dp.log.Debug().
					Uint64("ping_id", pingID).
					Msg("Another ping was successful, giving up on this one")
				return
			}
		}
	case <-reset.C:
		dp.log.Debug().
			Uint64("ping_id", pingID).
			Msg("Another ping was successful, giving up on this one")
		if timer != nil && !timer.Stop() {
			<-timer.C
		}
	case <-dp.stop:
		if timer != nil && !timer.Stop() {
			<-timer.C
		}
	}
}

func (dp *dittoPinger) Ping(pingID uint64, timeout time.Duration, timeoutCount int, reset *resetter) {
	if dp.client.SkipDittoPings {
		// Backgrounded-tab replica: send NO periodic NOTIFY_DITTO_ACTIVITY at all.
		// The ping's isActive flag is a routing signal with no good value for a
		// headless bridge: true keeps re-suppressing the phone's notifications
		// (rings/vibration) every minute, false revokes stream fan-out entirely.
		// A real backgrounded web tab sends neither — it asserts active once on
		// focus/load and then goes quiet, which keeps stream fan-out while the
		// phone's ring-suppression decays. Connection liveness is covered by the
		// receive idle read-deadline (~10s heartbeats, 30s deadline), and
		// SetActiveSession-on-connect + reassert-on-reopen still run (unlike
		// DontMarkActive, which skips those too).
		return
	}
	if dp.client.DontMarkActive {
		// Passive/background mode: skip the ditto-activity keepalive entirely so
		// we never assert active presence. The long-poll receive loop
		// self-maintains and keeps delivering messages (a backgrounded web tab
		// likewise sends no activity pings); auth-expiry is still caught via
		// ListenFatalError on the long-poll. The Loop's periodic data-receive
		// check remains as a slow-path recovery.
		return
	}
	dp.pingHandlingLock.Lock()
	if time.Since(dp.lastPingTime) < minPingInterval {
		dp.log.Debug().
			Uint64("ping_id", pingID).
			Time("last_ping_time", dp.lastPingTime).
			Msg("Skipping ping since last one was too recently")
		dp.pingHandlingLock.Unlock()
		return
	}
	now := time.Now()
	dp.lastPingTime = now
	if dp.oldestPingTime.IsZero() {
		dp.oldestPingTime = now
	}
	pingChan, err := dp.client.NotifyDittoActivity()
	if err != nil {
		dp.log.Err(err).Uint64("ping_id", pingID).Msg("Error sending ping")
		dp.pingFails++
		dp.client.triggerEvent(&events.PingFailed{
			Error:      fmt.Errorf("failed to notify ditto activity: %w", err),
			ErrorCount: dp.pingFails,
		})
		dp.pingHandlingLock.Unlock()
		return
	}
	dp.pingHandlingLock.Unlock()
	if dp.client.ReportInactive {
		// We just reported isActive=false. The server does not ack "inactive"
		// pings the way it acks active keepalives, so waiting for a response
		// would always time out and wrongly fire PhoneNotResponding plus
		// exponential back-off — which would starve the isActive=false signal
		// Google needs to keep notifying the phone. Fire-and-forget: drain any
		// late response so nothing blocks, and rely on the long-poll +
		// data-receive check for connection health.
		// Confirmed experimentally: an isActive=false ditto ping is NEVER acked
		// via the long-poll (it times out whether the long-poll is alive or dead),
		// so the response can't be used as a dead-long-poll detector in inactive
		// mode. Drain any late response so nothing blocks; connection health in
		// this mode is handled out-of-band by openmessage's periodic reconcile.
		go func() {
			select {
			case <-pingChan:
			case <-time.After(defaultPingTimeout):
			}
		}()
		return
	}
	if timeoutCount == 0 {
		dp.WaitForResponse(pingID, now, timeout, timeoutCount, pingChan, reset)
	} else {
		go dp.WaitForResponse(pingID, now, timeout, timeoutCount, pingChan, reset)
	}
}

const DefaultBugleDefaultCheckInterval = 2*time.Hour + 55*time.Minute

func (dp *dittoPinger) Loop() {
	var lastDataReceiveCheck time.Time
	for {
		var pingStart time.Time
		select {
		case <-dp.client.pingShortCircuit:
			pingID := pingIDCounter.Add(1)
			dp.log.Debug().Uint64("ping_id", pingID).Msg("Ditto ping wait short-circuited")
			pingStart = time.Now()
			dp.Ping(pingID, shortPingTimeout, 0, newResetter())
		case <-time.After(dp.pingInterval):
			pingID := pingIDCounter.Add(1)
			dp.log.Trace().Uint64("ping_id", pingID).Msg("Doing normal ditto ping")
			pingStart = time.Now()
			dp.Ping(pingID, defaultPingTimeout, 0, newResetter())
		case <-dp.stop:
			return
		}
		if dp.client.shouldDoDataReceiveCheck() {
			dp.log.Warn().
				Time("last_data_receive_check", lastDataReceiveCheck).
				Msg("No data received recently, sending extra GET_UPDATES call")
			go dp.HandleNoRecentUpdates()
			lastDataReceiveCheck = time.Now()
		} else if time.Since(pingStart) > 5*time.Minute || (time.Since(pingStart) > 1*time.Minute && time.Since(lastDataReceiveCheck) > 30*time.Minute) {
			dp.log.Warn().
				Time("ping_start", pingStart).
				Time("last_data_receive_check", lastDataReceiveCheck).
				Msg("Was disconnected for over a minute, sending extra GET_UPDATES call")
			go dp.HandleNoRecentUpdates()
			lastDataReceiveCheck = time.Now()
		}
	}
}

// reassertActiveSession re-blesses this session as an attended receiver after
// the ReceiveMessages stream is reopened. Confirmed experimentally: a freshly
// asserted session receives inbound over the stream, but after the stream
// reconnects (e.g. connection reset by peer) Google delivers nothing on the
// reopened stream — it stays healthy (HTTP 200, heartbeats) yet withholds all
// message frames until the session asserts activity again. Unlike
// SetActiveSession, the session ID is NOT reset: minting a new session fires
// the phone's "Device pairing" notification, while GET_UPDATES on the existing
// session is silent (the same call HandleNoRecentUpdates already makes).
func (c *Client) reassertActiveSession(log *zerolog.Logger) {
	err := c.sessionHandler.sendMessageNoResponse(SendMessageParams{
		Action:    gmproto.ActionType_GET_UPDATES,
		OmitTTL:   true,
		RequestID: c.sessionHandler.sessionID,
	})
	if err != nil {
		log.Err(err).Msg("Failed to re-assert active session after long-poll reopen")
	} else {
		log.Debug().Msg("Re-asserted active session after long-poll reopen")
	}
}

func (dp *dittoPinger) HandleNoRecentUpdates() {
	dp.client.triggerEvent(&events.NoDataReceived{})
	err := dp.client.sessionHandler.sendMessageNoResponse(SendMessageParams{
		Action:    gmproto.ActionType_GET_UPDATES,
		OmitTTL:   true,
		RequestID: dp.client.sessionHandler.sessionID,
	})
	if err != nil {
		dp.log.Err(err).Msg("Failed to send extra GET_UPDATES call")
	} else {
		dp.log.Debug().Msg("Sent extra GET_UPDATES call")
	}
}

func (c *Client) shouldDoDataReceiveCheck() bool {
	c.nextDataReceiveCheckLock.Lock()
	defer c.nextDataReceiveCheckLock.Unlock()
	if time.Until(c.nextDataReceiveCheck) <= 0 {
		c.nextDataReceiveCheck = time.Now().Add(DefaultBugleDefaultCheckInterval)
		return true
	}
	return false
}

func (c *Client) bumpNextDataReceiveCheck(after time.Duration) {
	c.nextDataReceiveCheckLock.Lock()
	if time.Until(c.nextDataReceiveCheck) < after {
		c.nextDataReceiveCheck = time.Now().Add(after)
	}
	c.nextDataReceiveCheckLock.Unlock()
}

func tryReadBody(resp io.ReadCloser) []byte {
	data, _ := io.ReadAll(resp)
	_ = resp.Close()
	return data
}

// receiveEndpoint captures the only two things that differ between the legacy
// ReceiveMessages long-poll and the modern PullMessages long-poll: the request
// body and the endpoint URL. The streaming response is assumed identical and is
// parsed by the shared readLongPoll (MED confidence — see MODERN_API.md §1.1;
// fork readLongPoll if a capture shows the modern framing is not `[[ … ]]`
// pblite).
type receiveEndpoint struct {
	name         string
	url          string
	urlGoogle    string
	buildPayload func(c *Client, listenReqID string) proto.Message
}

var receiveEndpointLegacy = receiveEndpoint{
	name:      "ReceiveMessages",
	url:       util.ReceiveMessagesURL,
	urlGoogle: util.ReceiveMessagesURLGoogle,
	buildPayload: func(c *Client, listenReqID string) proto.Message {
		return &gmproto.ReceiveMessagesRequest{
			Auth: &gmproto.AuthMessage{
				RequestID:        listenReqID,
				TachyonAuthToken: c.AuthData.TachyonAuthToken,
				Network:          c.AuthData.AuthNetwork(),
				ConfigVersion:    util.ConfigMessage,
			},
			Unknown: &gmproto.ReceiveMessagesRequest_UnknownEmptyObject2{
				Unknown: &gmproto.ReceiveMessagesRequest_UnknownEmptyObject1{},
			},
		}
	},
}

var receiveEndpointModern = receiveEndpoint{
	name:      "PullMessages",
	url:       util.PullMessagesURL,
	urlGoogle: util.PullMessagesURLGoogle,
	// TODO(modern-api): the real PullMessagesRequest layout below the header is
	// UNKNOWN (schema-less JS decode + encrypted capture). Per MODERN_API.md §4.2
	// step 2 we start from a ReceiveMessagesRequest CLONE — its auth/header blob
	// sits at field 1, which matches the one HIGH-confidence fact about the
	// modern request (header = 1). It very likely also needs a resume cursor /
	// ack-state field whose number is not yet known; add it here once captured.
	buildPayload: func(c *Client, listenReqID string) proto.Message {
		return &gmproto.ReceiveMessagesRequest{
			Auth: &gmproto.AuthMessage{
				RequestID:        listenReqID,
				TachyonAuthToken: c.AuthData.TachyonAuthToken,
				Network:          c.AuthData.AuthNetwork(),
				ConfigVersion:    util.ConfigMessage,
			},
			Unknown: &gmproto.ReceiveMessagesRequest_UnknownEmptyObject2{
				Unknown: &gmproto.ReceiveMessagesRequest_UnknownEmptyObject1{},
			},
		}
	},
}

// doLongPoll runs the legacy Messaging/ReceiveMessages receive loop.
func (c *Client) doLongPoll(loggedIn, background bool, onFirstConnect func()) bool {
	return c.pollReceive(receiveEndpointLegacy, loggedIn, background, onFirstConnect)
}

// doPullMessages runs the modern Messaging/PullMessages receive loop. It is the
// parallel of doLongPoll gated by Client.UseModernReceive. NOT yet validated
// against the live service — see docs/IMPLEMENTATION_NOTES.md.
func (c *Client) doPullMessages(loggedIn, background bool, onFirstConnect func()) bool {
	return c.pollReceive(receiveEndpointModern, loggedIn, background, onFirstConnect)
}

func (c *Client) pollReceive(endpoint receiveEndpoint, loggedIn, background bool, onFirstConnect func()) bool {
	c.listenID++
	listenID := c.listenID
	listenReqID := uuid.NewString()

	log := c.Logger.With().Int("listen_id", listenID).Logger()
	defer func() {
		log.Debug().Msg("Long polling stopped")
	}()
	ctx := log.WithContext(context.TODO())
	log.Debug().Str("listen_uuid", listenReqID).Msg("Long polling starting")

	if loggedIn {
		stopDittoPinger := make(chan struct{})
		defer close(stopDittoPinger)
		go (&dittoPinger{
			pingInterval:      c.pingInterval,
			alertTimeoutCount: c.alertTimeoutCount,
			stop:              stopDittoPinger,
			log:               &log,
			client:            c,
		}).Loop()
	}

	errorCount := 1
	for c.listenID == listenID {
		err := c.refreshAuthToken(nil)
		if err != nil {
			if exhttp.IsNetworkError(err) {
				if loggedIn {
					c.triggerEvent(&events.ListenTemporaryError{Error: fmt.Errorf("failed to refresh auth token: %w", err)})
				}
				errorCount++
				sleepSeconds := (errorCount + 1) * 5
				if background {
					if errorCount >= 3 {
						return false
					}
					sleepSeconds = errorCount * 2
				}
				log.Err(err).Int("sleep_seconds", sleepSeconds).Msg("Error refreshing auth token, retrying in a while")
				time.Sleep(time.Duration(sleepSeconds) * time.Second)
				continue
			}
			log.Err(err).Msg("Error refreshing auth token")
			if loggedIn {
				c.triggerEvent(&events.ListenFatalError{Error: fmt.Errorf("failed to refresh auth token: %w", err)})
			}
			return false
		}
		log.Trace().Str("receive_endpoint", endpoint.name).Msg("Starting new long-polling request")
		payload := endpoint.buildPayload(c, listenReqID)
		url := endpoint.url
		if c.AuthData.HasCookies() {
			url = endpoint.urlGoogle
		}
		resp, err := c.makeProtobufHTTPRequestContext(ctx, url, payload, ContentTypePBLite, true)
		if err != nil {
			if loggedIn {
				c.triggerEvent(&events.ListenTemporaryError{Error: err})
			}
			errorCount++
			sleepSeconds := (errorCount + 1) * 5
			if background {
				if errorCount >= 3 {
					return false
				}
				sleepSeconds = errorCount * 2
			}
			log.Err(err).Int("sleep_seconds", sleepSeconds).Msg("Error making listen request, retrying in a while")
			time.Sleep(time.Duration(sleepSeconds) * time.Second)
			continue
		}
		if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
			body := tryReadBody(resp.Body)
			log.Error().
				Int("status_code", resp.StatusCode).
				Bytes("resp_body", body).
				Msg("Error making listen request")
			if loggedIn {
				c.triggerEvent(&events.ListenFatalError{Error: events.HTTPError{Action: "polling", Resp: resp, Body: body}})
			}
			return false
		} else if resp.StatusCode >= 400 {
			if loggedIn {
				c.triggerEvent(&events.ListenTemporaryError{Error: events.HTTPError{Action: "polling", Resp: resp, Body: tryReadBody(resp.Body)}})
			} else {
				_ = resp.Body.Close()
			}
			errorCount++
			sleepSeconds := (errorCount + 1) * 5
			if background {
				if errorCount >= 3 {
					return false
				}
				sleepSeconds = errorCount * 2
			}
			log.Debug().
				Int("statusCode", resp.StatusCode).
				Int("sleep_seconds", sleepSeconds).
				Msg("Error in long polling, retrying in a while")
			time.Sleep(time.Duration(sleepSeconds) * time.Second)
			continue
		}
		if errorCount > 0 {
			errorCount = 0
			if loggedIn {
				c.triggerEvent(&events.ListenRecovered{})
			}
		}
		log.Debug().Int("statusCode", resp.StatusCode).Msg("Long polling opened")
		c.longPollingConn = resp.Body
		if onFirstConnect != nil {
			go onFirstConnect()
			onFirstConnect = nil
		} else if loggedIn && !c.DontMarkActive {
			// Re-assert this session as an attended receiver on every stream
			// reopen after the first (postConnect covers the first). Google
			// stops fanning out inbound messages to a session once its
			// ReceiveMessages stream is re-established without a fresh activity
			// assertion: the connection reopens fine (HTTP 200, heartbeats) but
			// delivers nothing. The real web client re-asserts on every tab
			// hidden→visible transition, so its reopens are always re-blessed.
			go c.reassertActiveSession(&log)
		}
		cleanClose := c.readLongPoll(&log, resp.Body, background)
		c.longPollingConn = nil
		if background {
			return cleanClose
		}
	}
	return true
}

func (c *Client) readLongPoll(log *zerolog.Logger, rc io.ReadCloser, background bool) bool {
	defer rc.Close()
	c.disconnecting = false
	reader := bufio.NewReader(rc)
	buf := make([]byte, 2621440)
	var accumulatedData []byte
	n, err := reader.Read(buf[:2])
	if err != nil {
		log.Err(err).Msg("Error reading opening bytes")
		return false
	} else if n != 2 || string(buf[:2]) != "[[" {
		log.Err(err).Msg("Opening is not [[")
		return false
	}
	var closeIn *time.Timer
	receivedEvents := false
	idleTimeout := receiveIdleTimeout
	if c.ReceiveIdleTimeout > 0 {
		idleTimeout = c.ReceiveIdleTimeout
	}
	onRead := func() {
		if closeIn == nil {
			return
		}
		if background {
			if receivedEvents {
				closeIn.Reset(3 * time.Second)
			} else {
				closeIn.Reset(5 * time.Second)
			}
		} else {
			// Foreground: a healthy ReceiveMessages stream emits a server
			// heartbeat every ~10s, so any frame (data or heartbeat) rearms the
			// idle deadline. See the timer setup below.
			closeIn.Reset(idleTimeout)
		}
	}
	if background {
		closeIn = time.NewTimer(10 * time.Second)
		go func() {
			<-closeIn.C
			c.closeLongPolling()
		}()
	} else {
		// Foreground dead-stream detector. The long-poll can go silently deaf —
		// no data, no heartbeat, no error (a half-open connection: LB/NAT idle
		// timeout, or the server dropping the stream) — and reader.Read below
		// would otherwise block forever, so inbound messages silently stop until
		// something else forces a reconnect. The server heartbeats a live stream
		// every ~10s (measured), so if no frame arrives within receiveIdleTimeout
		// (3 missed heartbeats) the stream is dead: close it to make the poll loop
		// reconnect. This is the liveness detection the real web client has.
		closeIn = time.NewTimer(idleTimeout)
		idleDone := make(chan struct{})
		defer close(idleDone)
		go func() {
			select {
			case <-closeIn.C:
				c.Logger.Warn().
					Dur("idle_timeout", idleTimeout).
					Msg("ReceiveMessages stream idle past deadline (no data/heartbeat) — closing to force reconnect")
				// Close only THIS poll's connection so reader.Read unblocks and the
				// pollReceive loop reopens. Do NOT call closeLongPolling(): that
				// bumps listenID, which would end the poll loop instead of
				// reconnecting it.
				_ = rc.Close()
			case <-idleDone:
				closeIn.Stop()
			}
		}()
	}
	var expectEOF bool
	for {
		n, err = reader.Read(buf)
		if err != nil {
			var logEvt *zerolog.Event
			if (errors.Is(err, io.EOF) && expectEOF) || c.disconnecting {
				logEvt = log.Trace()
			} else {
				logEvt = log.Warn()
			}
			logEvt.Err(err).Msg("Stopped reading data from server")
			return receivedEvents
		} else if expectEOF {
			log.Warn().Msg("Didn't get EOF after stream end marker")
		}
		onRead()
		chunk := buf[:n]
		if len(accumulatedData) == 0 {
			if len(chunk) == 2 && string(chunk) == "]]" {
				log.Trace().Msg("Got stream end marker")
				expectEOF = true
				continue
			}
			chunk = bytes.TrimPrefix(chunk, []byte{','})
		}
		accumulatedData = append(accumulatedData, chunk...)
		if !json.Valid(accumulatedData) {
			log.Trace().Msg("Invalid JSON, reading next chunk")
			continue
		}
		currentBlock := accumulatedData
		accumulatedData = accumulatedData[:0]
		msg := &gmproto.LongPollingPayload{}
		err = pblite.Unmarshal(currentBlock, msg)
		if err != nil {
			log.Err(err).Msg("Error deserializing pblite message")
			continue
		}
		switch {
		case msg.GetData() != nil:
			c.HandleRPCMsg(msg.GetData())
			receivedEvents = true
			onRead()
		case msg.GetAck() != nil:
			level := zerolog.TraceLevel
			if msg.GetAck().GetCount() > 0 {
				level = zerolog.DebugLevel
			}
			log.WithLevel(level).Int32("count", msg.GetAck().GetCount()).Msg("Got startup ack count message")
			c.skipCount = int(msg.GetAck().GetCount())
		case msg.GetStartRead() != nil:
			log.Trace().Msg("Got startRead message")
		case msg.GetHeartbeat() != nil:
			log.Trace().Msg("Got heartbeat message")
		default:
			log.Warn().
				Str("data", base64.StdEncoding.EncodeToString(currentBlock)).
				Msg("Got unknown message")
		}
	}
}

func (c *Client) closeLongPolling() {
	if conn := c.longPollingConn; conn != nil {
		c.Logger.Debug().Int("current_listen_id", c.listenID).Msg("Closing long polling connection manually")
		c.listenID++
		c.disconnecting = true
		_ = conn.Close()
		c.longPollingConn = nil
	}
}
