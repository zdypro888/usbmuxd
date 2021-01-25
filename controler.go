package usbmuxd

import (
	"log"
	"strings"
	"sync"
	"time"
)

//DeviceControler 设备处理
type DeviceControler struct {
	UserName     string
	Password     string
	Target       string
	Command      []string
	UpdateDEB    string
	UpdateFiles  []string
	RunApp       string
	InstallApp   string
	UninstallApp string
	Reboot       bool

	DeviceCount int
	Devices     *sync.Map

	OnPlug     func(*USBDevice) bool
	OnUnPlug   func(*USBDevice)
	OnProgress func(*USBDevice) error
}

//NeedSSH 是否需要SSH连接
func (controler *DeviceControler) NeedSSH() bool {
	return len(controler.Command) > 0 ||
		controler.UpdateDEB != "" ||
		len(controler.UpdateFiles) >= 3 ||
		controler.RunApp != "" ||
		controler.InstallApp != "" ||
		controler.UninstallApp != ""
}

//Listen 开启监听
func (controler *DeviceControler) Listen() error {
	controler.Devices = &sync.Map{}
	listener := &USBListener{
		Delegate: controler,
	}
	return listener.Listen()
}

//USBDeviceDidPlug 设备进入
func (controler *DeviceControler) USBDeviceDidPlug(frame *USBDeviceAttachedDetachedFrame) {
	if controler.Target != "" && controler.Target != frame.Properties.SerialNumber {
		log.Printf("device plug[%s] but not target", frame.Properties.SerialNumber)
		return
	}
	log.Printf("device plug[%d]: %s %x", controler.DeviceCount, frame.Properties.SerialNumber, frame.Properties.ProductID)
	controler.DeviceCount++
	device := &USBDevice{ID: frame.DeviceID, UDID: frame.Properties.SerialNumber, Product: frame.Properties.ProductID, Pluged: true}
	if controler.OnPlug != nil {
		if !controler.OnPlug(device) {
			return
		}
	}
	controler.Devices.Store(frame.DeviceID, device)
	go controler.progress(device)
}

//USBDeviceDidUnPlug 设备断开
func (controler *DeviceControler) USBDeviceDidUnPlug(frame *USBDeviceAttachedDetachedFrame) {
	log.Printf("device unplug[%d]: %s", controler.DeviceCount, frame.Properties.SerialNumber)
	if value, ok := controler.Devices.LoadAndDelete(frame.DeviceID); ok {
		controler.DeviceCount--
		device := value.(*USBDevice)
		device.Cancel()
		if controler.OnUnPlug != nil {
			controler.OnUnPlug(device)
		}
	}
}

//USBDidReceiveErrorWhilePluggingOrUnplugging 收到错误
func (controler *DeviceControler) USBDidReceiveErrorWhilePluggingOrUnplugging(err error, msg string) {
}

func (controler *DeviceControler) progress(device *USBDevice) {
	if len(controler.Command) > 0 {
		su := device.SSH(controler.UserName, controler.Password)
		defer su.Close()
		if err := su.ConnectSSH(); err != nil {
			log.Printf("[%s]connect ssh error: %v", device.UDID, err)
			return
		}
		if err := su.Command(controler.Command[0], controler.Command[1:]); err != nil {
			log.Printf("[%s]exec command error: %v", device.UDID, err)
			return
		}
		log.Printf("[%s]exec command finished", device.UDID)
		return
	}
	if controler.UpdateDEB != "" {
		su := device.SSH(controler.UserName, controler.Password)
		defer su.Close()
		if err := su.ConnectSSH(); err != nil {
			log.Printf("[%s]connect ssh error: %v", device.UDID, err)
			return
		}
		if err := su.ConnectSFTP(); err != nil {
			log.Printf("[%s]connect sftp error: %v", device.UDID, err)
			return
		}
		if err := su.InstallDEB(controler.UpdateDEB); err != nil {
			log.Printf("[%s]install deb error: %v", device.UDID, err)
			return
		}
		log.Printf("[%s]install deb finished", device.UDID)
		return
	}
	if len(controler.UpdateFiles) >= 3 {
		su := device.SSH(controler.UserName, controler.Password)
		defer su.Close()
		if err := su.ConnectSSH(); err != nil {
			log.Printf("[%s]connect ssh error: %v", device.UDID, err)
			return
		}
		if err := su.ConnectSFTP(); err != nil {
			log.Printf("[%s]connect sftp error: %v", device.UDID, err)
			return
		}
		if err := su.UploadFiles(controler.UpdateFiles[0], controler.UpdateFiles[1], controler.UpdateFiles[2:]); err != nil {
			log.Printf("[%s]upload file error: %v", device.UDID, err)
			return
		}
		log.Printf("[%s]file uploaded", device.UDID)
		return
	}
	if controler.RunApp != "" {
		if err := device.RunApp(controler.RunApp); err != nil {
			log.Printf("[%s]app run error:: %v", device.UDID, err)
		}
		return
	}
	if controler.InstallApp != "" {
		if err := device.InstallAPP(controler.InstallApp); err != nil {
			log.Printf("[%s]app install error: %v", device.UDID, err)
		}
		return
	}
	if controler.UninstallApp != "" {
		if err := device.UninstallAPP(controler.UninstallApp); err != nil {
			log.Printf("[%s]app uninstall error: %v", device.UDID, err)
		}
		return
	}
	if controler.Reboot {
		if err := device.Reboot(); err != nil {
			log.Printf("[%s]app uninstall error: %v", device.UDID, err)
		}
		return
	}
	if controler.OnProgress != nil {
		for device.Pluged {
			if err := controler.OnProgress(device); err != nil {
				log.Printf("device[%s]: callback error(%v)", device.UDID, err)
				if strings.Contains(err.Error(), ErrDevicePortUnavailable.Error()) {
					break
				}
				time.Sleep(5 * time.Second)
			} else {
				break
			}
		}
	}
}
