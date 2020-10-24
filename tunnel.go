// +build darwin dragonfly freebsd linux nacl netbsd openbsd solaris

package usbmuxd

import (
	"net"
	"os/exec"
	"path"
	"time"

	"github.com/kardianos/osext"
)

//Tunnel 通道
func Tunnel(d time.Duration) (net.Conn, error) {
	return net.DialTimeout("unix", "/var/run/usbmuxd", d)
}
func iMobileDeviceCMD(udid string) (string, string, string, string, string, error) {
	folder, err := osext.ExecutableFolder()
	if err != nil {
		return "", "", "", "", "", err
	}
	ideviceinfoCMD := exec.Command("ideviceinfo", "-u", udid, "-k", "ProductVersion")
	productVersion, err := ideviceinfoCMD.Output()
	if err != nil {
		return "", "", "", "", "", err
	}
	DeveloperDiskImage := path.Join(folder, "DeviceSupport", string(productVersion[:4]), "DeveloperDiskImage.dmg")
	return "ideviceinstaller", "ideviceimagemounter", DeveloperDiskImage, "idevicedebug", "idevicediagnostics", nil
}
