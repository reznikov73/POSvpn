module posvpn-server

go 1.27.1

require posvpn-engine v0.0.0

require (
	github.com/quic-go/quic-go v0.64.1 // indirect
	golang.org/x/crypto v0.54.0 // indirect
	golang.org/x/net v0.56.0 // indirect
	golang.org/x/sys v0.47.0 // indirect
)

replace posvpn-engine => ../engine
