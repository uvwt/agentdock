//go:build windows

package desktopruntime

import (
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"fmt"
	"net"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"unsafe"

	"golang.org/x/sys/windows"
)

type tunnelNetwork struct {
	key       string
	resolvers []string
}

var tunnelIPHelper = windows.NewLazySystemDLL("iphlpapi.dll")
var tunnelTCPTable = tunnelIPHelper.NewProc("GetExtendedTcpTable")
var tunnelRouteTable = tunnelIPHelper.NewProc("GetIpForwardTable")

// 指定的端口必须属于当前 child；另一个安装或抢占端口的程序不能提供就绪证据。
func tunnelMetricsOwnedBy(port, pid int) bool {
	if port <= 0 || pid <= 0 {
		return false
	}
	size := uint32(16384)
	for attempt := 0; attempt < 3; attempt++ {
		data := make([]byte, size)
		code, _, _ := tunnelTCPTable.Call(uintptr(unsafe.Pointer(&data[0])), uintptr(unsafe.Pointer(&size)), 0, windows.AF_INET, 3, 0)
		if windows.Errno(code) == windows.ERROR_INSUFFICIENT_BUFFER {
			continue
		}
		if code != 0 || len(data) < 4 {
			return false
		}
		count := int(binary.LittleEndian.Uint32(data))
		for i := 0; i < count && 4+(i+1)*24 <= len(data); i++ {
			row := data[4+i*24 : 4+(i+1)*24]
			if binary.LittleEndian.Uint32(row[4:]) == 0x0100007f && int(binary.BigEndian.Uint16(row[8:])) == port && int(binary.LittleEndian.Uint32(row[20:])) == pid {
				return true
			}
		}
		return false
	}
	return false
}

func currentTunnelNetwork() (tunnelNetwork, error) {
	size := uint32(16384)
	for attempt := 0; attempt < 3; attempt++ {
		data := make([]byte, size)
		first := (*windows.IpAdapterAddresses)(unsafe.Pointer(&data[0]))
		err := windows.GetAdaptersAddresses(windows.AF_UNSPEC, windows.GAA_FLAG_INCLUDE_GATEWAYS|windows.GAA_FLAG_SKIP_ANYCAST|windows.GAA_FLAG_SKIP_MULTICAST, 0, first, &size)
		if errors.Is(err, windows.ERROR_BUFFER_OVERFLOW) {
			continue
		}
		if err != nil {
			return tunnelNetwork{}, err
		}
		var records []string
		var resolvers []string
		bestMetric := ^uint32(0)
		for adapter := first; adapter != nil; adapter = adapter.Next {
			if adapter.OperStatus != windows.IfOperStatusUp || adapter.IfType == windows.IF_TYPE_SOFTWARE_LOOPBACK {
				continue
			}
			prefix := fmt.Sprintf("%d/%d/%d", adapter.IfIndex, adapter.Ipv4Metric, adapter.Ipv6Metric)
			records = append(records, prefix)
			for address := adapter.FirstUnicastAddress; address != nil; address = address.Next {
				records = append(records, prefix+"/ip/"+address.Address.IP().String())
			}
			for gateway := adapter.FirstGatewayAddress; gateway != nil; gateway = gateway.Next {
				records = append(records, prefix+"/gw/"+gateway.Address.IP().String())
			}
			var candidates []string
			for dns := adapter.FirstDnsServerAddress; dns != nil; dns = dns.Next {
				ip := dns.Address.IP()
				if ip == nil || ip.IsUnspecified() {
					continue
				}
				host := ip.String()
				if ip.To4() == nil && ip.IsLinkLocalUnicast() {
					host += "%" + strconv.FormatUint(uint64(adapter.Ipv6IfIndex), 10)
				}
				endpoint := net.JoinHostPort(host, "53")
				records = append(records, prefix+"/dns/"+endpoint)
				if len(candidates) < 10 {
					candidates = append(candidates, endpoint)
				}
			}
			// 使用当前优先接口的解析器直接查询，避开上一套 VPN 留下的系统 DNS 缓存。
			if len(candidates) > 0 && adapter.Ipv4Metric < bestMetric {
				bestMetric = adapter.Ipv4Metric
				resolvers = candidates
			}
		}
		runtime.KeepAlive(data)
		routes, err := tunnelDefaultRoutes()
		if err != nil {
			return tunnelNetwork{}, err
		}
		records = append(records, routes...)
		sort.Strings(records)
		return tunnelNetwork{key: fmt.Sprintf("%x", sha256.Sum256([]byte(strings.Join(records, "\n")))), resolvers: resolvers}, nil
	}
	return tunnelNetwork{}, fmt.Errorf("读取网络接口时配置持续变化")
}

func tunnelDefaultRoutes() ([]string, error) {
	size := uint32(16384)
	for attempt := 0; attempt < 3; attempt++ {
		data := make([]byte, size)
		code, _, _ := tunnelRouteTable.Call(uintptr(unsafe.Pointer(&data[0])), uintptr(unsafe.Pointer(&size)), 1)
		if windows.Errno(code) == windows.ERROR_INSUFFICIENT_BUFFER {
			continue
		}
		if code != 0 {
			return nil, windows.Errno(code)
		}
		var records []string
		count := int(binary.LittleEndian.Uint32(data))
		for i := 0; i < count && 4+(i+1)*56 <= len(data); i++ {
			row := data[4+i*56 : 4+(i+1)*56]
			mask := binary.LittleEndian.Uint32(row[4:])
			if mask == 0 || mask == 128 {
				records = append(records, fmt.Sprintf("route/%x/%x/%x", row[:20], row[20:28], row[36:40]))
			}
		}
		return records, nil
	}
	return nil, fmt.Errorf("读取默认路由时配置持续变化")
}
