package update

import _ "embed"

// DefaultPublicKey is the Ed25519 key release files are signed with
// (cmd/nd-sign). Shared by the Windows app and the Android app.
//
//go:embed update-pubkey.txt
var DefaultPublicKey string
