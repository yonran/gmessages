package util

const MessagesBaseURL = "https://messages.google.com"

const GoogleAuthenticationURL = MessagesBaseURL + "/web/authentication"
const GoogleTimesourceURL = MessagesBaseURL + "/web/timesource"

const instantMessagingBaseURL = "https://instantmessaging-pa.googleapis.com"
const instantMessagingBaseURLGoogle = "https://instantmessaging-pa.clients6.google.com"

const UploadMediaURL = instantMessagingBaseURL + "/upload"

const pairingBaseURL = instantMessagingBaseURL + "/$rpc/google.internal.communications.instantmessaging.v1.Pairing"
const RegisterPhoneRelayURL = pairingBaseURL + "/RegisterPhoneRelay"
const RefreshPhoneRelayURL = pairingBaseURL + "/RefreshPhoneRelay"
const GetWebEncryptionKeyURL = pairingBaseURL + "/GetWebEncryptionKey"
const RevokeRelayPairingURL = pairingBaseURL + "/RevokeRelayPairing"

const messagingBaseURL = instantMessagingBaseURL + "/$rpc/google.internal.communications.instantmessaging.v1.Messaging"
const messagingBaseURLGoogle = instantMessagingBaseURLGoogle + "/$rpc/google.internal.communications.instantmessaging.v1.Messaging"
const ReceiveMessagesURL = messagingBaseURL + "/ReceiveMessages"
const SendMessageURL = messagingBaseURL + "/SendMessage"
const AckMessagesURL = messagingBaseURL + "/AckMessages"
const ReceiveMessagesURLGoogle = messagingBaseURLGoogle + "/ReceiveMessages"
const SendMessageURLGoogle = messagingBaseURLGoogle + "/SendMessage"
const AckMessagesURLGoogle = messagingBaseURLGoogle + "/AckMessages"

// Modern-API receive path (messages.google.com/web PullMessages). The method
// names/paths are HIGH confidence (MODERN_API.md §2); the request/response
// BODIES below the header are not (see docs/IMPLEMENTATION_NOTES.md).
const PullMessagesURL = messagingBaseURL + "/PullMessages"
const PullMessagesURLGoogle = messagingBaseURLGoogle + "/PullMessages"

// Optional modern-API auxiliary endpoints (wrappers only observed; bodies
// UNKNOWN). Not used by the receive path yet — declared so callers/tests can
// reference the canonical paths. PrewarmReceiver rides the Messaging service.
const PrewarmReceiverURL = messagingBaseURL + "/PrewarmReceiver"
const PrewarmReceiverURLGoogle = messagingBaseURLGoogle + "/PrewarmReceiver"

const registrationBaseURL = instantMessagingBaseURLGoogle + "/$rpc/google.internal.communications.instantmessaging.v1.Registration"
const SignInGaiaURL = registrationBaseURL + "/SignInGaia"
const RegisterRefreshURL = registrationBaseURL + "/RegisterRefresh"

// ListIdentities is on the Registration service (MODERN_API.md §1.6); no prior
// art, body UNKNOWN.
const ListIdentitiesURL = registrationBaseURL + "/ListIdentities"

// GetFiUserStanding is on the MessagesMultiDevice service per the JS descriptors
// (MODERN_API.md §1.6). Not required for the receive path.
const multiDeviceBaseURL = instantMessagingBaseURLGoogle + "/$rpc/google.internal.communications.instantmessaging.v1.MessagesMultiDevice"
const GetFiUserStandingURL = multiDeviceBaseURL + "/GetFiUserStanding"

const ConfigURL = "https://messages.google.com/web/config"
