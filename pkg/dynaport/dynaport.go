// Package dynaport exposes functionality to get free (unused) ports on your
// system.
package dynaport

import (
	"errors"
	"math/rand"
	"net"
	"strconv"
	"sync"
)

const (
	lowPort   = 10000
	maxPorts  = 65535
	blockSize = 1024
	maxBlocks = 16
	attempts  = 10
)

var (
	port      int
	firstPort int
	once      sync.Once
	mu        sync.Mutex

	lockLn net.Listener
)

// Get returns n ports that are free to use, panicing if it can't succeed.
func Get(n int) []int {
	ports, err := GetWithErr(n)
	if err != nil {
		panic(err)
	}
	return ports
}

// GetS returns n ports as strings that are free to use, panicing if it can't succeed.
func GetS(n int) []string {
	ports, err := GetSWithErr(n)
	if err != nil {
		panic(err)
	}
	return ports
}

// GetSWithErr return n ports (as strings) that are free to use.
func GetSWithErr(n int) ([]string, error) {
	ports, err := GetWithErr(n)
	if err != nil {
		return nil, err
	}
	var portsStr []string
	for _, port := range ports {
		portsStr = append(portsStr, strconv.Itoa(port))
	}
	return portsStr, nil
}

// GetWithErr returns n ports that are free to use.
func GetWithErr(n int) ([]int, error) {
	mu.Lock()
	defer mu.Unlock()

	if n > blockSize-1 {
		return nil, errors.New("dynaport: block size is too small for ports requested")
	}

	once.Do(initialize)

	var ports []int
	for len(ports) < n {
		port++

		if port < firstPort+1 || port >= firstPort+blockSize {
			port = firstPort + 1
		}

		ln, err := listen(port)
		if err != nil {
			continue
		}
		ln.Close()

		ports = append(ports, port)
	}

	return ports, nil
}

func initialize() {
	if lowPort+maxBlocks*blockSize > maxPorts {
		panic("dynaport: block size is too big or too many blocks requested")
	}

	var err error
	for _ = range attempts {
		//nolint:gosec // This is a non-security-sensitive use of math/rand
		block := int(rand.Int31n(int32(maxBlocks)))
		firstPort = lowPort + block*blockSize
		lockLn, err = listen(firstPort)
		if err != nil {
			continue
		}
		return
	}
	panic("dynaport: can't allocated port block")
}

func listen(port int) (*net.TCPListener, error) {
	return net.ListenTCP("tcp", &net.TCPAddr{IP: net.ParseIP("127.0.0.1"), Port: port})
}
