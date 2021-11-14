# Reverse TCP tunnel
For TCP-based services that do not have inbound end point, `Reverse TCP Tunnel` allows us to build a reverse TCP tunnel to open in bound access from a public end point. It virutally extends the TCP listening port to a remote machine in which a `Reverse TCP Tunnel` listener is running.

We use a simple signaling protocol for tunnel establishment and connection multiplexing.

## Tunnel listener
Tunnel listener runs in a public end point, when it receives a `ListenRequest` from service that needs tunnelled inbound access, it opens a dynamic TCP port at public interface and multiplexes traffic between service client and the service provider.

Example command to launch tunnel listener
```bash
./tunnel -l 5555
```

## Tunnel Connector
Tunnel connector runs wihin the private network boundary, it has access to services that requires tunnelled inbound access.

Example command to establish a reverse tunnelling setup

```bash
./tunnel -c localhost:5555 www.myservice.com:80
```

## Build
```
go build
```
