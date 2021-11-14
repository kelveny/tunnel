package main

import (
	"bytes"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestSerializePdu(t *testing.T) {
	assert := require.New(t)

	pdu := &ListenRequest{
		proxyAddress: "www.google.com",
		proxyPort:    443,
	}

	b := bytes.NewBuffer(nil)
	serializePduTo(pdu, b)

	pduClone := serializePduFrom(bytes.NewBuffer(b.Bytes()))
	assert.True(pduClone != nil)
	assert.True(pduClone.(*ListenRequest).proxyAddress == "www.google.com")
	assert.True(pduClone.(*ListenRequest).proxyPort == 443)
}
