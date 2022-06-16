package usbmuxd

import (
	"flag"
	"fmt"
	"io"
	"net"
	"os"
	"path"
	"time"

	"github.com/pkg/sftp"
	"github.com/zdypro888/daemon"
	"github.com/zdypro888/utils"
	"golang.org/x/crypto/ssh"
)

//Dialer 拨号
type Dialer interface {
	DialTimeout(network, addr string, t time.Duration) (net.Conn, error)
}

//SSHUtil 设备SSH
type SSHUtil struct {
	UserName   string
	Password   string
	Network    string
	Address    string
	Dialer     Dialer
	sshclient  *ssh.Client
	sftpclient *sftp.Client
}

//ConnectSSH 连接SSH(错误: unable to authenticate)
func (su *SSHUtil) ConnectSSH() error {
	clientConfig := &ssh.ClientConfig{
		User:    su.UserName,
		Auth:    []ssh.AuthMethod{ssh.Password(su.Password)},
		Timeout: 30 * time.Second,
		HostKeyCallback: func(hostname string, remote net.Addr, key ssh.PublicKey) error {
			return nil
		},
	}
	if su.Dialer != nil {
		conn, err := su.Dialer.DialTimeout(su.Network, su.Address, clientConfig.Timeout)
		if err != nil {
			return err
		}
		c, chans, reqs, err := ssh.NewClientConn(conn, su.Address, clientConfig)
		if err != nil {
			return err
		}
		su.sshclient = ssh.NewClient(c, chans, reqs)
	} else {
		client, err := ssh.Dial(su.Network, su.Address, clientConfig)
		if err != nil {
			return err
		}
		su.sshclient = client
	}
	return nil
}

//Command 运行命令
func (su *SSHUtil) Command(command string, pipes []string) error {
	session, err := su.sshclient.NewSession()
	if err != nil {
		return err
	}
	defer session.Close()
	session.Stdout = os.Stdout
	session.Stderr = os.Stderr
	stdin, err := session.StdinPipe()
	if err != nil {
		return err
	}
	if err = session.Start(command); err != nil {
		return err
	}
	if pipes != nil {
		for _, arg := range pipes {
			stdin.Write([]byte(arg + "\n"))
		}
	}
	return session.Wait()
}

//ConnectSFTP 连接SFTP
func (su *SSHUtil) ConnectSFTP() error {
	sftpClient, err := sftp.NewClient(su.sshclient)
	if err != nil {
		return err
	}
	su.sftpclient = sftpClient
	return nil
}

//UploadSFTP 上传
func (su *SSHUtil) UploadSFTP(filePath string, toPath string) error {
	srcFile, err := os.Open(filePath)
	if err != nil {
		return err
	}
	defer srcFile.Close()
	su.sftpclient.Mkdir(path.Dir(toPath))
	dstFile, err := su.sftpclient.Create(toPath)
	if err != nil {
		return err
	}
	defer dstFile.Close()
	if _, err := io.Copy(dstFile, srcFile); err != nil {
		return err
	}
	return nil
}

//Close 关闭
func (su *SSHUtil) Close() {
	if su.sftpclient != nil {
		su.sftpclient.Close()
		su.sftpclient = nil
	}
	if su.sshclient != nil {
		su.sshclient.Close()
		su.sshclient = nil
	}
}

//InstallDEB 安装DEB
func (su *SSHUtil) InstallDEB(filePath string) error {
	toPath := path.Join("/var/mobile/", "install.deb")
	if err := su.UploadSFTP(filePath, toPath); err != nil {
		return err
	}
	if err := su.Command("rm -rf /var/lib/dpkg/updates/*", nil); err != nil {
		return err
	}
	if err := su.Command(fmt.Sprintf("dpkg -i \"%s\"", toPath), nil); err != nil {
		return err
	}
	//su.runCommand("/sbin/reboot")
	return nil
}

//UploadFiles 上传文件
func (su *SSHUtil) UploadFiles(localPath, remotePath string, files []string) error {
	for _, fileName := range files {
		localFilePath := path.Join(localPath, fileName)
		remoteFilePath := path.Join(remotePath, fileName)
		if err := su.UploadSFTP(localFilePath, remoteFilePath); err != nil {
			return err
		}
	}
	return nil
}

//Service 标准服务
func Service(name, description string, dependencies ...string) *DeviceControler {
	fUserName := flag.String("user", "root", "Password for devices")
	fPassword := flag.String("passwd", "", "Password for devices")
	fUUID := flag.String("udid", "", "UUID for target device")
	fCommand := flag.String("command", "", "Command for execute")
	fUpdateDeb := flag.String("update", "", "Update for install.deb")
	fUpdateFile := flag.String("upload", "", "Upload files. localpath,remotepath,files")
	fReboot := flag.Bool("reboot", false, "Reboot device")
	fRunApp := flag.String("apprun", "", "Run ios app")
	fInstallApp := flag.String("appinstall", "", "path for ipa to install")
	fUninstallApp := flag.String("appuninstall", "", "BundleID for uninstall")
	if !daemon.RunWithConsole(name, description, dependencies...) {
		return nil
	}
	controler := &DeviceControler{}
	controler.UserName = *fUserName
	controler.Password = *fPassword
	controler.Target = *fUUID
	controler.Command = utils.SplitWithoutEmpty(*fCommand, ",")
	controler.UpdateDEB = *fUpdateDeb
	controler.Reboot = *fReboot
	controler.RunApp = *fRunApp
	controler.InstallApp = *fInstallApp
	controler.UninstallApp = *fUninstallApp
	controler.UpdateFiles = utils.SplitWithoutEmpty(*fUpdateFile, ",")
	return controler
}
