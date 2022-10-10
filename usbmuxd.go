package usbmuxd

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"os/exec"
	"strconv"
	"sync/atomic"
	"time"

	"github.com/zdypro888/go-plist"
)

//调试过程:
/*
	sudo mv /var/run/usbmuxd /var/run/usbmuxx
	sudo socat -t100 tcp-listen:11111,reuseaddr,fork unix-connect:/var/run/usbmuxx
	sudo socat -t100 unix-listen:/var/run/usbmuxd,mode=777,reuseaddr,fork tcp:127.0.0.1:11111
*/

// ErrDeviceDisconnected 设备未找到错误
var ErrDeviceDisconnected = errors.New("device has disconnected")

// ErrDevicePortUnavailable 端口不正确
var ErrDevicePortUnavailable = errors.New("Port you're requesting is unavailable")

// ErrDevicePortUnknow 未知错误
var ErrDevicePortUnknow = errors.New("[IDK]: Malformed request received in the device")

// USBListenRequestFrame When we want to listen for any new USB device or device removed
type USBListenRequestFrame struct {
	MessageType         string `plist:"MessageType"`
	ClientVersionString string `plist:"ClientVersionString"`
	ProgName            string `plist:"ProgName"`
}

// USBGenericACKFrame Its a frame model for generic response after we send listen or connect
// Number == 0 {OK}, Number == 1 {Device not connected anymore}, Number == 2 {Port not available}, Number == 5 {IDK}
type USBGenericACKFrame struct {
	MessageType string `plist:"MessageType"`
	Number      int    `plist:"Number"`
}

// USBDeviceAttachedDetachedFrame Model for USB connect or disconnect frame
type USBDeviceAttachedDetachedFrame struct {
	MessageType string                               `plist:"MessageType"`
	DeviceID    int                                  `plist:"DeviceID"`
	Properties  USBDeviceAttachedPropertiesDictFrame `plist:"Properties"`
}

// USBDeviceAttachedPropertiesDictFrame Model for USB attach properties
type USBDeviceAttachedPropertiesDictFrame struct {
	ConnectionSpeed int    `plist:"ConnectionSpeed"`
	ConnectionType  string `plist:"ConnectionType"`
	DeviceID        int    `plist:"DeviceID"`
	LocationID      int    `plist:"LocationID"`
	ProductID       int    `plit:"ProductID"`
	SerialNumber    string `plit:"SerialNumber"`
}

// USBConnectRequestFrame Model for connect frame to a specific port in a connected device
type USBConnectRequestFrame struct {
	MessageType         string `plist:"MessageType"`
	ClientVersionString string `plist:"ClientVersionString"`
	ProgName            string `plist:"ProgName"`
	DeviceID            int    `plist:"DeviceID"`
	PortNumber          int    `plist:"PortNumber"`
}

type usbmuxdHeader struct {
	Length  uint32 // length of the header + plist (16 + plist.length)
	Version uint32 // 0 for binary version, 1 for plist version
	Request uint32 // always 8 (taken from tcprelay.py)
	Tag     uint32 // always 1 (taken from tcprelay.py)
}

func (header *usbmuxdHeader) Bytes(data []byte) []byte {
	header.Length = uint32(16 + len(data))
	buffer := make([]byte, header.Length)
	binary.LittleEndian.PutUint32(buffer, header.Length)
	binary.LittleEndian.PutUint32(buffer[4:], header.Version)
	binary.LittleEndian.PutUint32(buffer[8:], header.Request)
	binary.LittleEndian.PutUint32(buffer[12:], header.Tag)
	copy(buffer[16:], data)
	return buffer
}
func (header *usbmuxdHeader) Command(frame any) ([]byte, error) {
	data := &bytes.Buffer{}
	encoder := plist.NewEncoder(data)
	if err := encoder.Encode(frame); err != nil {
		return nil, err
	}
	return header.Bytes(data.Bytes()), nil
}
func (header *usbmuxdHeader) Parser(data []byte, frame any) error {
	header.Version = binary.LittleEndian.Uint32(data[0:4])
	header.Request = binary.LittleEndian.Uint32(data[4:8])
	header.Tag = binary.LittleEndian.Uint32(data[8:12])
	decoder := plist.NewDecoder(bytes.NewReader(data[12:]))
	return decoder.Decode(frame)
}

func createHeader() *usbmuxdHeader {
	return &usbmuxdHeader{Version: 1, Request: 8, Tag: 1}
}

// USBDeviceDelegate 回调
type USBDeviceDelegate interface {
	USBDeviceDidPlug(*USBDeviceAttachedDetachedFrame)
	USBDeviceDidUnPlug(*USBDeviceAttachedDetachedFrame)
	USBDidReceiveErrorWhilePluggingOrUnplugging(error, string)
}

// USBListener usbmuxd监听
type USBListener struct {
	Delegate USBDeviceDelegate
	running  uint32
}

func (listener *USBListener) listenGo() {
	for atomic.LoadUint32(&listener.running) == 1 {
		if conn, err := Tunnel(5 * time.Second); err != nil {
			log.Printf("open usbmuxd tunnel error: %v", err)
			time.Sleep(5 * time.Second)
		} else {
			header := createHeader()
			if buf, err := header.Command(&USBListenRequestFrame{
				MessageType:         "Listen",
				ProgName:            "go-usbmuxd",
				ClientVersionString: "1.0.0",
			}); err != nil {
				panic(fmt.Errorf("create header buffer error: %v", err))
			} else if _, err = conn.Write(buf); err != nil {
				log.Printf("write listen header buffer error: %v", err)
				time.Sleep(5 * time.Second)
			} else {
				header := &usbmuxdHeader{}
				var frame USBGenericACKFrame
				lenBuf := make([]byte, 4)
				devices := make(map[int]*USBDeviceAttachedDetachedFrame)
				for atomic.LoadUint32(&listener.running) == 1 {
					if _, err := io.ReadFull(conn, lenBuf); err != nil {
						log.Printf("read buffer len error: %v", err)
						break
					}
					pbuf := make([]byte, binary.LittleEndian.Uint32(lenBuf)-4)
					if _, err := io.ReadFull(conn, pbuf); err != nil {
						log.Printf("read buffer error: %v", err)
						break
					}
					if err := header.Parser(pbuf, &frame); err != nil {
						listener.Delegate.USBDidReceiveErrorWhilePluggingOrUnplugging(err, string(pbuf))
					} else if frame.MessageType == "Result" {
						if frame.Number != 0 {
							listener.Delegate.USBDidReceiveErrorWhilePluggingOrUnplugging(errors.New("Illegal response received"), string(pbuf))
						}
					} else {
						data := &USBDeviceAttachedDetachedFrame{}
						if err := header.Parser(pbuf, data); err != nil {
							listener.Delegate.USBDidReceiveErrorWhilePluggingOrUnplugging(err, string(pbuf))
						} else if data.MessageType == "Attached" {
							devices[data.DeviceID] = data
							listener.Delegate.USBDeviceDidPlug(data)
						} else if data.MessageType == "Detached" {
							listener.Delegate.USBDeviceDidUnPlug(data)
							delete(devices, data.DeviceID)
						} else {
							listener.Delegate.USBDidReceiveErrorWhilePluggingOrUnplugging(errors.New("Unable to parse the response"), string(pbuf))
						}
					}
				}
				for _, data := range devices {
					listener.Delegate.USBDeviceDidUnPlug(data)
				}
				time.Sleep(5 * time.Second)
			}
			conn.Close()
		}
	}
	atomic.StoreUint32(&listener.running, 0)
}

// Listen 监听设备
func (listener *USBListener) Listen() error {
	if atomic.CompareAndSwapUint32(&listener.running, 0, 1) {
		go listener.listenGo()
		return nil
	}
	return fmt.Errorf("listener not closed: %d", listener.running)
}

// Close 监听关闭
func (listener *USBListener) Close() {
	atomic.StoreUint32(&listener.running, 2)
}

// USBDevice 客户端
type USBDevice struct {
	ID      int
	UDID    string
	Product int
	Pluged  bool
	Object  any
}

func byteSwap(val int) int {
	return ((val & 0xFF) << 8) | ((val >> 8) & 0xFF)
}

// Connect 连接
func (device *USBDevice) Connect(port int, d time.Duration) (net.Conn, error) {
	var err error
	var conn net.Conn
	if conn, err = Tunnel(d); err != nil {
		return nil, err
	}
	hasError := true
	defer func() {
		if hasError {
			conn.Close()
		}
	}()
	header := createHeader()
	var buf []byte
	if buf, err = header.Command(&USBConnectRequestFrame{
		DeviceID:            device.ID,
		PortNumber:          byteSwap(port),
		MessageType:         "Connect",
		ClientVersionString: "1.0.0",
		ProgName:            "go-usbmuxd",
	}); err != nil {
		return nil, err
	}
	if _, err = conn.Write(buf); err != nil {
		return nil, err
	}
	lenBuf := make([]byte, 4)
	if _, err = io.ReadFull(conn, lenBuf); err != nil {
		return nil, err
	}
	pbuf := make([]byte, binary.LittleEndian.Uint32(lenBuf)-4)
	if _, err = io.ReadFull(conn, pbuf); err != nil {
		return nil, err
	}
	var frame USBGenericACKFrame
	if err = header.Parser(pbuf, &frame); err != nil {
		return nil, err
	} else if frame.MessageType != "Result" {
		return nil, fmt.Errorf("unknow message type: %s", frame.MessageType)
	} else {
		switch frame.Number {
		case 0:
			hasError = false
			return conn, nil
		case 2:
			// Device Disconnected
			return nil, ErrDeviceDisconnected
		case 3:
			// Port isn't available/ busy
			return nil, ErrDevicePortUnavailable
		case 5:
			// UNKNOWN Error
			return nil, ErrDevicePortUnknow
		default:
			return nil, fmt.Errorf("Unknow error code: %d", frame.Number)
		}
	}
}

// DialTimeout 连接
func (device *USBDevice) DialTimeout(network, addr string, t time.Duration) (net.Conn, error) {
	if network != "usbmuxd" {
		return nil, fmt.Errorf("Can not support: %s", network)
	}
	port, err := strconv.Atoi(addr)
	if err != nil {
		return nil, err
	}
	return device.Connect(port, t)
}

// Dial 连接
func (device *USBDevice) Dial(addr string, t time.Duration) (net.Conn, error) {
	return device.DialTimeout("usbmuxd", addr, t)
}

// RunApp 运行 APP
func (device *USBDevice) RunApp(bundleID string) error {
	_, ideviceimagemounter, DeveloperDiskImage, idevicedebug, _, err := iMobileDeviceCMD(device.UDID)
	if err != nil {
		log.Printf("device[%s]: error: %v", device.UDID, err)
		return err
	}
	log.Printf("device[%s]: app mounter", device.UDID)
	ideviceimagemounterCMD := exec.Command(ideviceimagemounter, "-u", device.UDID, DeveloperDiskImage)
	ideviceimagemounterCMD.Run()
	log.Printf("device[%s]: app start", device.UDID)
	idevicedebugCXT, idevicedebugCancel := context.WithTimeout(context.Background(), 40*time.Second)
	defer idevicedebugCancel()
	idevicedebugCMD := exec.CommandContext(idevicedebugCXT, idevicedebug, "-u", device.UDID, "run", bundleID)
	idevicedebugCMD.Run()
	log.Printf("device[%s]: app started", device.UDID)
	return nil
}

// InstallAPP 安装 app
func (device *USBDevice) InstallAPP(ipa string) error {
	ideviceinstaller, _, _, _, _, err := iMobileDeviceCMD(device.UDID)
	if err != nil {
		log.Printf("device[%s]: error: %v", device.UDID, err)
		return err
	}
	log.Printf("device[%s]: app install", device.UDID)
	ideviceinstallerCXT, ideviceinstallerCancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer ideviceinstallerCancel()
	ideviceinstallerCMD := exec.CommandContext(ideviceinstallerCXT, ideviceinstaller, "-u", device.UDID, "-i", ipa)
	ideviceinstallerCMD.Run()
	log.Printf("device[%s]: app installed", device.UDID)
	return nil
}

// UninstallAPP 卸载 app
func (device *USBDevice) UninstallAPP(appid string) error {
	ideviceinstaller, _, _, _, _, err := iMobileDeviceCMD(device.UDID)
	if err != nil {
		log.Printf("device[%s]: error: %v", device.UDID, err)
		return err
	}
	log.Printf("device[%s]: app uninstall", device.UDID)
	ideviceinstallerCXT, ideviceinstallerCancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer ideviceinstallerCancel()
	ideviceinstallerCMD := exec.CommandContext(ideviceinstallerCXT, ideviceinstaller, "-u", device.UDID, "-U", appid)
	ideviceinstallerCMD.Run()
	log.Printf("device[%s]: app uninstalled", device.UDID)
	return nil
}

// Reboot 重新启动
func (device *USBDevice) Reboot() error {
	_, _, _, _, idevicediagnostics, err := iMobileDeviceCMD(device.UDID)
	if err != nil {
		log.Printf("device[%s]: error: %v", device.UDID, err)
		return err
	}
	log.Printf("device[%s]: reboot", device.UDID)
	idevicediagnosticsCXT, idevicediagnosticsCancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer idevicediagnosticsCancel()
	idevicediagnosticsCMD := exec.CommandContext(idevicediagnosticsCXT, idevicediagnostics, "-u", device.UDID)
	idevicediagnosticsCMD.Run()
	log.Printf("device[%s]: app uninstalled", device.UDID)
	return nil
}

// Cancel 取消
func (device *USBDevice) Cancel() {
	device.Pluged = false
}

// SSH SSH连接
func (device *USBDevice) SSH(username, passowrd string) *SSHUtil {
	return &SSHUtil{
		UserName: username,
		Password: passowrd,
		Network:  "usbmuxd",
		Address:  "22",
		Dialer:   device,
	}
}
