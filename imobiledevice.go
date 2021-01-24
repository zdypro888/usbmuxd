package usbmuxd

import (
	"os/exec"
	"path"

	"github.com/kardianos/osext"
)

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
