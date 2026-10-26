module posvpn-client

go 1.27.1

require posvpn-engine v0.0.0

require (
	golang.org/x/sys v0.49.0 // indirect
	golang.org/x/term v0.47.0 // indirect
	golang.zx2c4.com/wintun v0.0.0-20230126152724-0fa3db229ce2 // indirect
	golang.zx2c4.com/wireguard/windows v1.1.1 // indirect
)

replace posvpn-engine => ../engine
