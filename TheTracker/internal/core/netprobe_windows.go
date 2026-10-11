package core

// The Windows calls behind the connection check. All of them work without
// admin rights and only read: ping (ICMP echo), the default gateway, the
// adapters' byte counters and the table of open TCP connections.

import (
	"errors"
	"net"
	"strings"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

var (
	iphlpapi                = windows.NewLazySystemDLL("iphlpapi.dll")
	procIcmpCreateFile      = iphlpapi.NewProc("IcmpCreateFile")
	procIcmpCloseHandle     = iphlpapi.NewProc("IcmpCloseHandle")
	procIcmpSendEcho        = iphlpapi.NewProc("IcmpSendEcho")
	procGetExtendedTcpTable = iphlpapi.NewProc("GetExtendedTcpTable")
)

type windowsProbe struct{}

// Ping sends one ICMP echo and returns the round trip, or false on no reply.
func (windowsProbe) Ping(ip string, timeout time.Duration) (time.Duration, bool) {
	addr := net.ParseIP(ip).To4()
	if addr == nil {
		return 0, false
	}
	h, _, _ := procIcmpCreateFile.Call()
	if h == 0 || h == uintptr(windows.InvalidHandle) {
		return 0, false
	}
	defer procIcmpCloseHandle.Call(h)
	dest := uint32(addr[0]) | uint32(addr[1])<<8 | uint32(addr[2])<<16 | uint32(addr[3])<<24
	payload := []byte("thetracker")
	// ICMP_ECHO_REPLY plus the echoed data and room for an ICMP error.
	reply := make([]byte, 64+len(payload)+8)
	n, _, _ := procIcmpSendEcho.Call(h, uintptr(dest), uintptr(unsafe.Pointer(&payload[0])), uintptr(len(payload)), 0,
		uintptr(unsafe.Pointer(&reply[0])), uintptr(len(reply)), uintptr(timeout.Milliseconds()))
	if n == 0 {
		return 0, false
	}
	status := *(*uint32)(unsafe.Pointer(&reply[4]))
	rtt := *(*uint32)(unsafe.Pointer(&reply[8]))
	if status != 0 {
		return 0, false
	}
	return time.Duration(rtt) * time.Millisecond, true
}

// adapters lists the connected adapters that have a default gateway: the
// ones the internet goes through.
func (windowsProbe) adapters() ([]*windows.IpAdapterAddresses, error) {
	size := uint32(16 * 1024)
	for i := 0; i < 3; i++ {
		buf := make([]byte, size)
		first := (*windows.IpAdapterAddresses)(unsafe.Pointer(&buf[0]))
		err := windows.GetAdaptersAddresses(windows.AF_UNSPEC, windows.GAA_FLAG_INCLUDE_GATEWAYS, 0, first, &size)
		if err == windows.ERROR_BUFFER_OVERFLOW {
			continue
		}
		if err != nil {
			return nil, err
		}
		var out []*windows.IpAdapterAddresses
		for a := first; a != nil; a = a.Next {
			if a.OperStatus == windows.IfOperStatusUp && a.FirstGatewayAddress != nil {
				out = append(out, a)
			}
		}
		// Keep the buffer alive with the list.
		_ = buf
		return out, nil
	}
	return nil, errors.New("adapter list too large")
}

func (p windowsProbe) Gateway() (string, error) {
	list, err := p.adapters()
	if err != nil {
		return "", err
	}
	for _, a := range list {
		for g := a.FirstGatewayAddress; g != nil; g = g.Next {
			if ip := g.Address.IP(); ip != nil && ip.To4() != nil && !ip.IsUnspecified() {
				return ip.String(), nil
			}
		}
	}
	return "", errors.New("no default gateway")
}

// LineBytes adds up the bytes received and sent by the adapters with a
// gateway.
func (p windowsProbe) LineBytes() (in, out uint64, err error) {
	list, err := p.adapters()
	if err != nil {
		return 0, 0, err
	}
	for _, a := range list {
		row := windows.MibIfRow2{InterfaceIndex: a.IfIndex}
		if windows.GetIfEntry2Ex(0, &row) == nil {
			in += row.InOctets
			out += row.OutOctets
		}
	}
	return in, out, nil
}

// Connections counts each program's open TCP connections to the internet
// (loopback and the local network left out), by program name.
func (windowsProbe) Connections() map[string]int {
	names := processNames()
	out := map[string]int{}
	add := func(pid uint32, remote net.IP) {
		if remote.IsLoopback() || remote.IsPrivate() || remote.IsLinkLocalUnicast() || remote.IsUnspecified() {
			return
		}
		if n := names[pid]; n != "" {
			out[n]++
		}
	}
	const established = 5
	const tcpTableOwnerPIDConnections = 4
	for _, family := range []uint32{windows.AF_INET, windows.AF_INET6} {
		var size uint32
		procGetExtendedTcpTable.Call(0, uintptr(unsafe.Pointer(&size)), 0, uintptr(family), tcpTableOwnerPIDConnections, 0)
		if size == 0 {
			continue
		}
		buf := make([]byte, size+1024)
		size = uint32(len(buf))
		if r, _, _ := procGetExtendedTcpTable.Call(uintptr(unsafe.Pointer(&buf[0])), uintptr(unsafe.Pointer(&size)), 0, uintptr(family), tcpTableOwnerPIDConnections, 0); r != 0 {
			continue
		}
		count := int(*(*uint32)(unsafe.Pointer(&buf[0])))
		if family == windows.AF_INET {
			// MIB_TCPROW_OWNER_PID: state, local addr, local port, remote
			// addr, remote port, pid — six DWORDs.
			for i := 0; i < count; i++ {
				off := 4 + i*24
				if off+24 > len(buf) {
					break
				}
				row := (*[6]uint32)(unsafe.Pointer(&buf[off]))
				if row[0] != established {
					continue
				}
				a := row[3]
				add(row[5], net.IPv4(byte(a), byte(a>>8), byte(a>>16), byte(a>>24)))
			}
		} else {
			// MIB_TCP6ROW_OWNER_PID: local addr[16], scope, port, remote
			// addr[16], scope, port, state, pid — 56 bytes.
			for i := 0; i < count; i++ {
				off := 4 + i*56
				if off+56 > len(buf) {
					break
				}
				row := buf[off : off+56]
				state := *(*uint32)(unsafe.Pointer(&row[48]))
				if state != established {
					continue
				}
				remote := net.IP(append([]byte{}, row[24:40]...))
				add(*(*uint32)(unsafe.Pointer(&row[52])), remote)
			}
		}
	}
	return out
}

// processNames maps process ids to program names.
func processNames() map[uint32]string {
	out := map[uint32]string{}
	snap, err := windows.CreateToolhelp32Snapshot(windows.TH32CS_SNAPPROCESS, 0)
	if err != nil {
		return out
	}
	defer windows.CloseHandle(snap)
	var e windows.ProcessEntry32
	e.Size = uint32(unsafe.Sizeof(e))
	for err = windows.Process32First(snap, &e); err == nil; err = windows.Process32Next(snap, &e) {
		out[e.ProcessID] = strings.TrimSuffix(windows.UTF16ToString(e.ExeFile[:]), ".exe")
	}
	return out
}
