package main

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"net"
)

const (
	PDU_LISTEN_REQUEST             = 1
	PDU_LISTEN_RESPONSE            = 2
	PDU_TUNNEL_CONNECT_REQUEST     = 3
	PDU_TUNNEL_CONNECT_RESPONSE    = 4
	PDU_TUNNEL_DATA_INDICATION     = 5
	PDU_TUNNEL_DISCONNECT_REQUEST  = 6
	PDU_TUNNEL_DISCONNECT_RESPONSE = 7
)

type Serializable interface {
	GetSerialType() int
	GetSerialLength() uint32
	SerializeTo(w *bytes.Buffer)
	SerializeFrom(r *bytes.Buffer)
}

func serializeUInt32To(v uint32, w *bytes.Buffer) {
	b := make([]byte, 4)
	binary.BigEndian.PutUint32(b, v)
	w.Write(b)
}

func serializeUInt32From(r *bytes.Buffer) uint32 {
	b := make([]byte, 4)
	r.Read(b)
	return binary.BigEndian.Uint32(b)
}

func getStringSerialLength(s string) uint32 {
	return uint32(4 + len([]byte(s)))
}

func serializeStringTo(s string, w *bytes.Buffer) {
	l := uint32(len([]byte(s)))
	serializeUInt32To(l, w)
	w.Write([]byte(s))
}

func serializeStringFrom(r *bytes.Buffer) string {
	l := serializeUInt32From(r)

	b := make([]byte, int(l))
	r.Read(b)
	return string(b)
}

func getPduSerialLength(pdu Serializable) uint32 {
	return 1 + pdu.GetSerialLength()
}

func serializePduTo(pdu Serializable, w *bytes.Buffer) {
	w.WriteByte(byte(pdu.GetSerialType()))
	pdu.SerializeTo(w)
}

func serializePduFrom(r *bytes.Buffer) Serializable {
	t, _ := r.ReadByte()
	switch int(t) {
	case PDU_LISTEN_REQUEST:
		pdu := &ListenRequest{}
		pdu.SerializeFrom(r)
		return pdu

	case PDU_LISTEN_RESPONSE:
		pdu := &ListenResponse{}
		pdu.SerializeFrom(r)
		return pdu

	case PDU_TUNNEL_CONNECT_REQUEST:
		pdu := &TunnelConnectRequest{}
		pdu.SerializeFrom(r)
		return pdu

	case PDU_TUNNEL_CONNECT_RESPONSE:
		pdu := &TunnelConnectResponse{}
		pdu.SerializeFrom(r)
		return pdu

	case PDU_TUNNEL_DATA_INDICATION:
		pdu := &TunnelDataIndication{}
		pdu.SerializeFrom(r)
		return pdu

	case PDU_TUNNEL_DISCONNECT_REQUEST:
		pdu := &TunnelDisconnectRequest{}
		pdu.SerializeFrom(r)
		return pdu

	case PDU_TUNNEL_DISCONNECT_RESPONSE:
		pdu := &TunnelDisconnectResponse{}
		pdu.SerializeFrom(r)
		return pdu
	}

	fmt.Printf("Invalid protocol data\n")
	return nil
}

func sendPdu(conn net.Conn, pdu Serializable) error {
	l := getPduSerialLength(pdu)

	b := make([]byte, 4)
	binary.BigEndian.PutUint32(b, l)
	_, err := conn.Write(b)
	if err != nil {
		return err
	}

	buf := bytes.NewBuffer(nil)
	serializePduTo(pdu, buf)

	_, err = conn.Write(buf.Bytes())

	return err
}

/////////////////////////////////////////////////////////////////////////////

type ListenRequest struct {
	proxyAddress string
	proxyPort    int
}

func (pdu *ListenRequest) GetSerialType() int {
	return PDU_LISTEN_REQUEST
}

func (pdu *ListenRequest) GetSerialLength() uint32 {
	return 4 + getStringSerialLength(pdu.proxyAddress)
}

func (pdu *ListenRequest) SerializeTo(w *bytes.Buffer) {
	serializeStringTo(pdu.proxyAddress, w)
	serializeUInt32To(uint32(pdu.proxyPort), w)
}

func (pdu *ListenRequest) SerializeFrom(r *bytes.Buffer) {
	pdu.proxyAddress = serializeStringFrom(r)
	pdu.proxyPort = int(serializeUInt32From(r))
}

/////////////////////////////////////////////////////////////////////////////

type ListenResponse struct {
	proxyAddress  string
	proxyPort     int
	tunnelAddress string
	tunnelPort    int
}

func (pdu *ListenResponse) GetSerialType() int {
	return PDU_LISTEN_RESPONSE
}

func (pdu *ListenResponse) GetSerialLength() uint32 {
	return 8 + getStringSerialLength(pdu.proxyAddress) + getStringSerialLength(pdu.tunnelAddress)
}

func (pdu *ListenResponse) SerializeTo(w *bytes.Buffer) {
	serializeStringTo(pdu.proxyAddress, w)
	serializeUInt32To(uint32(pdu.proxyPort), w)
	serializeStringTo(pdu.tunnelAddress, w)
	serializeUInt32To(uint32(pdu.tunnelPort), w)
}

func (pdu *ListenResponse) SerializeFrom(r *bytes.Buffer) {
	pdu.proxyAddress = serializeStringFrom(r)
	pdu.proxyPort = int(serializeUInt32From(r))
	pdu.tunnelAddress = serializeStringFrom(r)
	pdu.tunnelPort = int(serializeUInt32From(r))
}

/////////////////////////////////////////////////////////////////////////////

// listener -> proxy
type TunnelConnectRequest struct {
	dataConnectionHandle uint32
	clientAddress        string

	proxyAddress string
	proxyPort    int
}

func (pdu *TunnelConnectRequest) GetSerialType() int {
	return PDU_TUNNEL_CONNECT_REQUEST
}

func (pdu *TunnelConnectRequest) GetSerialLength() uint32 {
	return 4 +
		getStringSerialLength(pdu.clientAddress) +
		getStringSerialLength(pdu.proxyAddress) +
		4
}

func (pdu *TunnelConnectRequest) SerializeTo(w *bytes.Buffer) {
	serializeUInt32To(uint32(pdu.dataConnectionHandle), w)
	serializeStringTo(pdu.clientAddress, w)
	serializeStringTo(pdu.proxyAddress, w)
	serializeUInt32To(uint32(pdu.proxyPort), w)
}

func (pdu *TunnelConnectRequest) SerializeFrom(r *bytes.Buffer) {
	pdu.dataConnectionHandle = Handle(serializeUInt32From(r))
	pdu.clientAddress = serializeStringFrom(r)
	pdu.proxyAddress = serializeStringFrom(r)
	pdu.proxyPort = int(serializeUInt32From(r))
}

/////////////////////////////////////////////////////////////////////////////

type TunnelConnectResponse struct {
	dataConnectionHandle  uint32
	proxyConnectionHandle uint32
}

func (pdu *TunnelConnectResponse) GetSerialType() int {
	return PDU_TUNNEL_CONNECT_RESPONSE
}

func (pdu *TunnelConnectResponse) GetSerialLength() uint32 {
	return 8
}

func (pdu *TunnelConnectResponse) SerializeTo(w *bytes.Buffer) {
	serializeUInt32To(uint32(pdu.dataConnectionHandle), w)
	serializeUInt32To(uint32(pdu.proxyConnectionHandle), w)
}

func (pdu *TunnelConnectResponse) SerializeFrom(r *bytes.Buffer) {
	pdu.dataConnectionHandle = serializeUInt32From(r)
	pdu.proxyConnectionHandle = serializeUInt32From(r)
}

/////////////////////////////////////////////////////////////////////////////

type TunnelDataIndication struct {
	peerConnectionHandle uint32
	data                 []byte
}

func (pdu *TunnelDataIndication) GetSerialType() int {
	return PDU_TUNNEL_DATA_INDICATION
}

func (pdu *TunnelDataIndication) GetSerialLength() uint32 {
	return uint32(4 + 4 + len(pdu.data))
}

func (pdu *TunnelDataIndication) SerializeTo(w *bytes.Buffer) {
	serializeUInt32To(uint32(pdu.peerConnectionHandle), w)
	serializeUInt32To(uint32(len(pdu.data)), w)
	w.Write(pdu.data)
}

func (pdu *TunnelDataIndication) SerializeFrom(r *bytes.Buffer) {
	pdu.peerConnectionHandle = serializeUInt32From(r)

	l := serializeUInt32From(r)
	pdu.data = make([]byte, int(l))
	r.Read(pdu.data)
}

/////////////////////////////////////////////////////////////////////////////

type TunnelDisconnectRequest struct {
	peerConnectionHandle uint32
}

func (pdu *TunnelDisconnectRequest) GetSerialType() int {
	return PDU_TUNNEL_DISCONNECT_REQUEST
}

func (pdu *TunnelDisconnectRequest) GetSerialLength() uint32 {
	return 4
}

func (pdu *TunnelDisconnectRequest) SerializeTo(w *bytes.Buffer) {
	serializeUInt32To(uint32(pdu.peerConnectionHandle), w)
}

func (pdu *TunnelDisconnectRequest) SerializeFrom(r *bytes.Buffer) {
	pdu.peerConnectionHandle = serializeUInt32From(r)
}

/////////////////////////////////////////////////////////////////////////////

type TunnelDisconnectResponse struct {
	peerConnectionHandle uint32
}

func (pdu *TunnelDisconnectResponse) GetSerialType() int {
	return PDU_TUNNEL_DISCONNECT_RESPONSE
}

func (pdu *TunnelDisconnectResponse) GetSerialLength() uint32 {
	return 4
}

func (pdu *TunnelDisconnectResponse) SerializeTo(w *bytes.Buffer) {
	serializeUInt32To(uint32(pdu.peerConnectionHandle), w)
}

func (pdu *TunnelDisconnectResponse) SerializeFrom(r *bytes.Buffer) {
	pdu.peerConnectionHandle = serializeUInt32From(r)
}

/////////////////////////////////////////////////////////////////////////////
