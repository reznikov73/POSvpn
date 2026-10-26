module posvpn-client

go 1.27.1

require (
	golang.org/x/sys v0.49.0
	golang.org/x/term v0.47.0
	golang.zx2c4.com/wintun v0.0.0-20230126152724-0fa3db229ce2
	golang.zx2c4.com/wireguard/windows v1.1.1
	posvpn-engine v0.0.0
)

require (
	github.com/quic-go/quic-go v0.64.1 // indirect
	golang.org/x/crypto v0.54.0 // indirect
	golang.org/x/net v0.56.0 // indirect
)

replace posvpn-engine => ../engine
