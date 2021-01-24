// +build darwin dragonfly freebsd linux nacl netbsd openbsd solaris

package usbmuxd

import (
	"net"
	"time"
)

//Tunnel 通道
func Tunnel(d time.Duration) (net.Conn, error) {
	return net.DialTimeout("unix", "/var/run/usbmuxd", d)
}
