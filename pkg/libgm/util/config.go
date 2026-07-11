package util

import (
	"go.mau.fi/mautrix-gmessages/pkg/libgm/gmproto"
)

var ConfigMessage = &gmproto.ConfigVersion{
	Year:  2026,
	Month: 3,
	Day:   18,
	V1:    4,
	V2:    6,
}

const QRNetwork = "Bugle"
const GoogleNetwork = "GDitto"

var BrowserDetailsMessage = &gmproto.BrowserDetails{
	UserAgent:   UserAgent,
	BrowserType: gmproto.BrowserType_OTHER,
	OS:          "libgm",
	// WEB (companion) instead of the historical TABLET(2). Live capture of the
	// real messages.google.com/web client shows it registers deviceType=WEB; a
	// TABLET device reads to Google as a standalone device that owns the thread,
	// which suppresses the phone's own notification, whereas a WEB companion lets
	// the phone keep notifying. This is set in BrowserDetails at PAIR time and is
	// NOT re-sent on reconnect, so it only takes effect after a fresh (re-)pair.
	// Single-variable change (only DeviceType) so the effect is attributable.
	// See docs/CAPTURED_FINDINGS.md. Unverified against the phone until re-paired.
	DeviceType: gmproto.DeviceType_WEB,
}
