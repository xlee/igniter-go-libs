package tun2socks

import (
	"io"
	"net"
	"os"
	"strings"
	"time"

	"github.com/eycorsican/go-tun2socks/common/dns/cache"
	"github.com/eycorsican/go-tun2socks/common/dns/fakedns"
	"github.com/eycorsican/go-tun2socks/common/log"
	"github.com/eycorsican/go-tun2socks/core"
	"github.com/eycorsican/go-tun2socks/proxy/socks"
	"github.com/trojan-gfw/igniter-go-libs/tun2socks/simpleandroidlog" // Register a simple android logger.

	"github.com/songgao/water"
)

var (
	lwipWriter        io.Writer
	lwipStack         core.LWIPStack
	mtuUsed           int
	stopSignalChannel chan bool
	stopReplyChannel  chan bool
	tunDev            *water.Interface
)

// Stop stop it
func Stop() {
	log.Infof("enter stop")
	log.Infof("begin close tun")
	err := tunDev.Close()
	if err != nil {
		log.Infof("close tun: %v", err)
	}
	log.Infof("send stop sig")
	close(stopSignalChannel)
	log.Infof("stop sig sent")
	<-stopReplyChannel
	if lwipStack != nil {
		log.Infof("begin close lwipstack")
		lwipStack.Close()
		lwipStack = nil
	}
}

// hack to receive tunfd
func openTunDevice(tunFd int) (*water.Interface, error) {
	file := os.NewFile(uintptr(tunFd), "tun") // dummy file path name since we already got the fd
	tunDev = &water.Interface{
		ReadWriteCloser: file,
	}
	return tunDev, nil
}

// DataPipeWorker generator
func createDataPipeWorker() chan bool {
	// a stop signal channel
	c := make(chan bool)

	// Copy packets from tun device to lwip stack, it's the main loop.
	go func(c <-chan bool) {
		var ok bool
	Loop:
		for {
			select {
			case _, ok = <-c:
				if !ok {
					log.Infof("got DataPipe stop signal")
					break Loop
				}

			default:
				// tun -> lwip
				_, err := io.CopyBuffer(lwipWriter, tunDev, make([]byte, mtuUsed))
				if err != nil {
					log.Infof("copying data failed: %v", err)
				}
			}

		}
		log.Infof("exit DataPipe loop")
		close(stopReplyChannel)
	}(c)

	return c
}

// Start sets up lwIP stack, starts a Tun2socks instance
func Start(tunFd int, socks5Server string, fakeIPStart string, fakeIPStop string, mtu int) int {

	mtuUsed = mtu
	var err error
	tunDev, err = openTunDevice(tunFd)
	if err != nil {
		log.Fatalf("failed to open tun device: %v", err)
	}

	if lwipStack == nil {
		// Setup the lwIP stack.
		lwipStack = core.NewLWIPStack()
		lwipWriter = lwipStack.(io.Writer)
	}

	// Register tun2socks connection handlers.
	proxyAddr, err := net.ResolveTCPAddr("tcp", socks5Server)
	proxyHost := proxyAddr.IP.String()
	proxyPort := uint16(proxyAddr.Port)
	if err != nil {
		log.Infof("invalid proxy server address: %v", err)
		return -1
	}
	cacheDNS := cache.NewSimpleDnsCache()
	if fakeIPStart != "" && fakeIPStop != "" {
		fakeDNS := fakedns.NewSimpleFakeDns(fakeIPStart, fakeIPStop)
		core.RegisterTCPConnHandler(socks.NewTCPHandler(proxyHost, proxyPort, fakeDNS, nil))
		core.RegisterUDPConnHandler(socks.NewUDPHandler(proxyHost, proxyPort, 30*time.Second, cacheDNS, fakeDNS, nil))
	} else {
		core.RegisterTCPConnHandler(socks.NewTCPHandler(proxyHost, proxyPort, nil, nil))
		core.RegisterUDPConnHandler(socks.NewUDPHandler(proxyHost, proxyPort, 30*time.Second, cacheDNS, nil, nil))
	}

	// Register an output callback to write packets output from lwip stack to tun
	// device, output function should be set before input any packets.
	core.RegisterOutputFn(func(data []byte) (int, error) {
		// lwip -> tun
		return tunDev.Write(data)
	})

	stopReplyChannel = make(chan bool)
	stopSignalChannel = createDataPipeWorker()

	log.Infof("Running tun2socks")

	return 0
}

// SetLoglevel set tun2socks log level
// possible input: debug/info/warn/error/none
func SetLoglevel(logLevel string) {
	// Set log level.
	switch strings.ToLower(logLevel) {
	case "debug":
		log.SetLevel(log.DEBUG)
	case "info":
		log.SetLevel(log.INFO)
	case "warn":
		log.SetLevel(log.WARN)
	case "error":
		log.SetLevel(log.ERROR)
	case "none":
		log.SetLevel(log.NONE)
	default:
		panic("unsupport logging level")
	}
	logger := simpleandroidlog.GetLogger()
	log.Infof("LogLevel: %v", logger.GetLevel())
}
