module github.com/zdypro888/usbmuxd

go 1.15

require (
	github.com/kardianos/osext v0.0.0-20190222173326-2bc1f35cddc0
	github.com/pkg/sftp v1.12.0
	github.com/takama/daemon v1.0.0 // indirect
	github.com/zdypro888/crash v0.0.0-20201024053043-5c61911cef88 // indirect
	github.com/zdypro888/daemon v0.0.0-20201024053526-642ab50db2b3
	github.com/zdypro888/utils v0.0.0-20201024062924-ef98c7e6c65f
	golang.org/x/crypto v0.0.0-20201016220609-9e8e0b390897
	howett.net/plist v0.0.0-20200419221736-3b63eb3a43b5
)


replace howett.net/plist => github.com/zdypro888/go-plist v1.2.2
