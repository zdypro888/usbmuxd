// +build windows

package usbmuxd

import (
	"net"
	"time"
)

//Tunnel 通道
func Tunnel(d time.Duration) (net.Conn, error) {
	return net.DialTimeout("tcp", "localhost:27015", d)
}
