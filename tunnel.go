package main

import (
	"bytes"
	"context"
	"encoding/binary"
	"flag"
	"fmt"
	"net"
	"strconv"
	"strings"
	"sync"
)

type Handle = uint32

/////////////////////////////////////////////////////////////////////////////

type tunnelProvider struct {
	lock sync.Mutex

	// map handle -> *TunnelConnection
	tunnelConnections map[Handle]*TunnelConnection

	// map handle -> *DataConnection
	dataConnections map[Handle]*DataConnection

	nextHandle Handle
}

func newTunnelProvider() *tunnelProvider {
	return &tunnelProvider{
		tunnelConnections: make(map[Handle]*TunnelConnection),
		dataConnections:   make(map[Handle]*DataConnection),
		nextHandle:        1,
	}
}

func (p *tunnelProvider) getNextHandle() Handle {
	p.lock.Lock()
	defer p.lock.Unlock()

	return p.getNextHandleUnLocked()
}

func (p *tunnelProvider) getNextHandleUnLocked() Handle {
	r := p.nextHandle
	p.nextHandle++

	return r
}

func (p *tunnelProvider) newTunnelConnection(conn net.Conn) *TunnelConnection {
	ctx, cancel := context.WithCancel(context.Background())
	tc := &TunnelConnection{
		provider: p,
		conn:     conn,
		ctx:      ctx,
		cancel:   cancel,
	}

	p.lock.Lock()
	defer p.lock.Unlock()

	handle := p.getNextHandleUnLocked()
	tc.handle = handle

	p.tunnelConnections[handle] = tc
	return tc
}

func (p *tunnelProvider) closeTunnelConnection(tc *TunnelConnection) {
	p.lock.Lock()
	defer p.lock.Unlock()

	delete(p.tunnelConnections, tc.handle)
}

func (p *tunnelProvider) getTunnelConnection(handle Handle) *TunnelConnection {
	p.lock.Lock()
	defer p.lock.Unlock()

	if tc, ok := p.tunnelConnections[handle]; ok {
		return tc
	}

	return nil
}

func (p *tunnelProvider) getAndClearTunnelConnection(handle Handle) *TunnelConnection {
	p.lock.Lock()
	defer p.lock.Unlock()

	if tc, ok := p.tunnelConnections[handle]; ok {
		delete(p.tunnelConnections, handle)
		return tc
	}

	return nil
}

func (p *tunnelProvider) newDataConnection(tc *TunnelConnection, conn net.Conn) *DataConnection {
	ctx, cancel := context.WithCancel(context.Background())
	dc := &DataConnection{
		conn: conn,

		tunnelConnection: tc,
		ctx:              ctx,
		cancel:           cancel,
	}

	p.lock.Lock()
	defer p.lock.Unlock()

	handle := p.getNextHandleUnLocked()
	dc.handle = handle

	p.dataConnections[handle] = dc
	return dc
}

func (p *tunnelProvider) closeDataConnection(dc *DataConnection, notifyPeer bool) {
	dc = p.getAndClearDataConnection(dc.handle)
	if dc != nil {
		fmt.Printf("Close data connection, local handle: %d, peer handle: %d\n",
			dc.handle, dc.peerHandle)

		dc.conn.Close()

		if notifyPeer {
			pdu := &TunnelDisconnectRequest{
				peerConnectionHandle: dc.peerHandle,
			}
			sendPdu(dc.tunnelConnection.conn, pdu)
		}
	}
}

func (p *tunnelProvider) getDataConnection(handle Handle) *DataConnection {
	p.lock.Lock()
	defer p.lock.Unlock()

	if dc, ok := p.dataConnections[handle]; ok {
		return dc
	}

	return nil
}

func (p *tunnelProvider) startListener(port int) {
	l, err := net.Listen("tcp4", fmt.Sprintf("0.0.0.0:%d", port))
	if err != nil {
		fmt.Printf("TCP listen error: %v\n", err)
		return
	}

	go func() {
		for {
			conn, err := l.Accept()
			if err != nil {
				fmt.Printf("TCP accept error: %v\n", err)
				break
			} else {
				tc := p.newTunnelConnection(conn)
				tc.open()
			}
		}

		l.Close()
	}()
}

func (p *tunnelProvider) startConnector(providerAddress string) (*TunnelConnection, error) {
	conn, err := net.Dial("tcp4", providerAddress)
	if err != nil {
		return nil, err
	}

	tc := p.newTunnelConnection(conn)
	tc.open()

	return tc, nil
}

func (p *tunnelProvider) getAndClearDataConnection(handle Handle) *DataConnection {
	p.lock.Lock()
	defer p.lock.Unlock()

	if dc, ok := p.dataConnections[handle]; ok {
		delete(p.dataConnections, handle)
		return dc
	}

	return nil
}

func (p *tunnelProvider) onTunnelPacket(tc *TunnelConnection, data []byte) {
	r := bytes.NewBuffer(data)
	pdu := serializePduFrom(r)
	if pdu != nil {
		switch int(pdu.GetSerialType()) {
		case PDU_LISTEN_REQUEST:
			tc.onListenRequest(pdu.(*ListenRequest))

		case PDU_LISTEN_RESPONSE:
			tc.onListenResponse(pdu.(*ListenResponse))

		case PDU_TUNNEL_CONNECT_REQUEST:
			tc.onTunnelConnectRequest(pdu.(*TunnelConnectRequest))

		case PDU_TUNNEL_CONNECT_RESPONSE:
			tc.onTunnelConnectResponse(pdu.(*TunnelConnectResponse))

		case PDU_TUNNEL_DATA_INDICATION:
			tc.onTunnelDataIndication(pdu.(*TunnelDataIndication))

		case PDU_TUNNEL_DISCONNECT_REQUEST:
			tc.onTunnelDisconnectRequest(pdu.(*TunnelDisconnectRequest))

		case PDU_TUNNEL_DISCONNECT_RESPONSE:
			tc.onTunnelDisconnectResponse(pdu.(*TunnelDisconnectResponse))
		}
	}
}

/////////////////////////////////////////////////////////////////////////////

type DataConnection struct {
	conn       net.Conn
	handle     Handle
	peerHandle Handle

	tunnelConnection *TunnelConnection
	ctx              context.Context
	cancel           context.CancelFunc
}

func (dc *DataConnection) open(peerHandle Handle) {
	dc.peerHandle = peerHandle

	go func() {
		b := make([]byte, 4096)
		for {
			sz, err := dc.conn.Read(b)

			if sz == 0 || err != nil {
				dc.close(true)
				return
			}

			pdu := &TunnelDataIndication{
				peerConnectionHandle: dc.peerHandle,
				data:                 b[0:sz],
			}

			// multiplex through tunnel connection
			sendPdu(dc.tunnelConnection.conn, pdu)
		}
	}()
}

func (dc *DataConnection) close(notifyPeer bool) {
	dc.tunnelConnection.provider.closeDataConnection(dc, notifyPeer)
}

/////////////////////////////////////////////////////////////////////////////

type TunnelConnection struct {
	provider *tunnelProvider
	conn     net.Conn
	handle   Handle

	tunnelPort int

	proxyAddress string
	proxyPort    int

	ctx    context.Context
	cancel context.CancelFunc
}

func (tc *TunnelConnection) startListenFor(proxyAddress string, proxyPort int) int {
	tc.proxyAddress = proxyAddress
	tc.proxyPort = proxyPort

	listener, _ := net.Listen("tcp4", ":0")
	tc.tunnelPort = listener.Addr().(*net.TCPAddr).Port

	go func() {
		for {
			c, err := listener.Accept()
			if err != nil {
				return
			}

			tc.onIncomingDataConnection(c)
		}
	}()

	return tc.tunnelPort
}

func (tc *TunnelConnection) startTunnelFor(proxyAddress string, proxyPort int) {
	tc.proxyAddress = proxyAddress
	tc.proxyPort = proxyPort

	pdu := &ListenRequest{
		proxyAddress: proxyAddress,
		proxyPort:    proxyPort,
	}

	sendPdu(tc.conn, pdu)
}

func (tc *TunnelConnection) onListenRequest(pdu *ListenRequest) {
	tunnelPort := tc.startListenFor(pdu.proxyAddress, pdu.proxyPort)

	responsePdu := &ListenResponse{
		tunnelAddress: "0.0.0.0",
		tunnelPort:    tunnelPort,
		proxyAddress:  pdu.proxyAddress,
		proxyPort:     pdu.proxyPort,
	}

	sendPdu(tc.conn, responsePdu)
}

func (tc *TunnelConnection) onListenResponse(pdu *ListenResponse) {
	tc.tunnelPort = pdu.tunnelPort

	fmt.Printf("Tunnel port is open: %d\n", pdu.tunnelPort)
}

func (tc *TunnelConnection) onTunnelConnectRequest(pdu *TunnelConnectRequest) {
	conn, err := net.Dial("tcp4", fmt.Sprintf("%s:%d", tc.proxyAddress, tc.proxyPort))

	if err != nil {
		response := &TunnelDisconnectResponse{
			peerConnectionHandle: pdu.dataConnectionHandle,
		}

		sendPdu(tc.conn, response)
		return
	}

	dc := tc.provider.newDataConnection(tc, conn)
	dc.open(pdu.dataConnectionHandle)

	fmt.Printf("Open data connection to target %s:%d. local handle: %d, peer handle: %d\n",
		tc.proxyAddress, tc.proxyPort, dc.handle, pdu.dataConnectionHandle)

	response := &TunnelConnectResponse{
		dataConnectionHandle:  pdu.dataConnectionHandle,
		proxyConnectionHandle: dc.handle,
	}
	sendPdu(tc.conn, response)
}

func (tc *TunnelConnection) onTunnelConnectResponse(pdu *TunnelConnectResponse) {
	if dc := tc.provider.getDataConnection(pdu.dataConnectionHandle); dc != nil {
		dc.open(pdu.proxyConnectionHandle)

		fmt.Printf("Connect data connection to target %s:%d. local handle: %d, peer handle: %d\n",
			tc.proxyAddress, tc.proxyPort, dc.handle, pdu.proxyConnectionHandle)
	}
}

func (tc *TunnelConnection) onTunnelDataIndication(pdu *TunnelDataIndication) {
	if dc := tc.provider.getDataConnection(pdu.peerConnectionHandle); dc != nil {
		_, err := dc.conn.Write(pdu.data)

		if err != nil {
			dc.close(true)
		}
	}
}

func (tc *TunnelConnection) onTunnelDisconnectRequest(pdu *TunnelDisconnectRequest) {
	fmt.Printf("Tunnel disconnect request for local handle: %d\n", pdu.peerConnectionHandle)

	if dc := tc.provider.getDataConnection(pdu.peerConnectionHandle); dc != nil {
		dc.close(false)

		response := &TunnelDisconnectResponse{
			peerConnectionHandle: dc.peerHandle,
		}
		sendPdu(tc.conn, response)
	}
}

func (tc *TunnelConnection) onTunnelDisconnectResponse(pdu *TunnelDisconnectResponse) {
	fmt.Printf("Tunnel disconnect response for local handle: %d\n", pdu.peerConnectionHandle)

	if dc := tc.provider.getDataConnection(pdu.peerConnectionHandle); dc != nil {
		dc.close(false)
	}
}

func (tc *TunnelConnection) onIncomingDataConnection(conn net.Conn) {
	dc := tc.provider.newDataConnection(tc, conn)

	req := &TunnelConnectRequest{
		dataConnectionHandle: dc.handle,
		clientAddress:        "0.0.0.0", // TODO

		proxyAddress: tc.proxyAddress,
		proxyPort:    tc.proxyPort,
	}

	sendPdu(tc.conn, req)
}

func (tc *TunnelConnection) open() {
	go func() {
		for {
			b := make([]byte, 4)
			len, err := tc.conn.Read(b)
			if len < 4 || err != nil {
				tc.provider.closeTunnelConnection(tc)
				break
			}

			dataLength := binary.BigEndian.Uint32(b)
			data := make([]byte, dataLength)
			len, err = tc.conn.Read(data)

			if len < int(dataLength) || err != nil {
				tc.provider.closeTunnelConnection(tc)
				break
			}

			tc.provider.onTunnelPacket(tc, data)
		}
	}()
}

func main() {
	port := flag.Int("l", 0, "Tunnel provider signaling port")
	providerAddress := flag.String("c", "", "Tunnel provider signaling address")
	targetAddress := flag.String("t", "", "Target address to be tunnelled")

	flag.Parse()

	p := newTunnelProvider()

	if *port != 0 {
		p.startListener(*port)

		// no graceful shutdown yet
		select {}
	} else {
		if len(*providerAddress) == 0 || len(*targetAddress) == 0 {
			fmt.Printf("Usage: tunnel [-l] [[-c] [-t]]\n")
			return
		}

		tc, err := p.startConnector(*providerAddress)
		if err != nil {
			fmt.Printf("Error: %s\n", err)
			return
		}

		addr := strings.Split(*targetAddress, ":")
		targetPort := 443
		if len(addr) > 1 {
			targetPort, _ = strconv.Atoi(addr[1])
		}

		tc.startTunnelFor(addr[0], targetPort)

		// no graceful shutdown yet
		select {}
	}
}
