package electrum

import (
	"encoding/json"
	"fmt"
	"net"
	"sync"
	"time"

	"github.com/rivine/rivine/types"
)

const (
	// connectionTimeout is the amount of time we wait for a client
	// to send any request, before we disconnect them for being idle
	connectionTimeout = time.Minute * 10
	// jsonRPCVersion is the value that MUST be specified in the JSONRPC field,
	// as per the JSONRPC 2.0 spec. Abscense, or any other value, mean that the
	// request is invalid
	jsonRPCVersion = "2.0"
)

type (
	// RPCTransport defines connections to clients which can serve RPC. The encoding
	// and decoding is handled as well
	RPCTransport interface {
		// GetMessage receives a single rpc request from the
		// underlying connection.
		GetRequest() <-chan *BatchRequest
		// GetError exposes errors generated by the transport,
		// or when decoding requests.
		GetError() <-chan error
		// Send a response or notification
		Send(interface{}) error
		// IsClosed checks if the connections is closed or closing.
		IsClosed() <-chan struct{}
		// Close the underlying connection and any goroutines
		// used to e.g. read from the connection
		Close(*sync.WaitGroup) error
		// RemoteAddr returns the remote address of the connection
		RemoteAddr() net.Addr
	}

	// errTransportClosed indicates that the underlying transport is closed
	errTransportClosed struct {
		error
	}

	// Request is the structure of an rpc call in JSONRPC 2.0. The JSONRPC field
	// value must be exactly "2.0", any other value, or its absence, is an error
	Request struct {
		JSONRPC string           `json:"jsonrpc"`
		Method  string           `json:"method"`
		Params  *json.RawMessage `json:"params"`
		ID      interface{}      `json:"id"`
	}

	// Response is the structure of an rpc response in JSONRPC 2.0. The JSONRPC
	// field value must be exactly "2.0". The ID value must be identical to the
	// Request that is being responded to
	Response struct {
		JSONRPC string      `json:"jsonrpc"`
		Result  interface{} `json:"result,omitempty"`
		Error   *RPCError   `json:"error,omitempty"`
		ID      interface{} `json:"id"`
	}

	// RPCError is the response for an errored rpc call
	RPCError struct {
		Code    int64       `json:"code"`
		Message string      `json:"message"`
		Data    interface{} `json:"data,omitempty"`
	}

	// Notification is a message send from the server to the client,
	// which should not be responded to
	Notification struct {
		JSONRPC string      `json:"jsonrpc"`
		Method  string      `json:"method"`
		Params  interface{} `json:"params"`
	}
)

func (err RPCError) Error() string {
	return fmt.Sprintf("Error %v: %v", err.Code, err.Message)
}

func newResponse(ID interface{}, result interface{}, err error) *Response {
	// If ID is not set, this was a notification and no result should be send
	if ID == nil {
		return nil
	}

	r := &Response{
		JSONRPC: jsonRPCVersion,
		ID:      ID,
	}

	// Since we can't set a value for result if the call errored,
	// check for an error first, and only set the result if the error
	// is not nil

	var rpcError RPCError
	if err != nil {
		if rpce, ok := err.(RPCError); ok {
			rpcError = rpce
		} else {
			// Convert to RPCError
			rpcError.Code = 1 // TODO: define custom error codes
			rpcError.Message = err.Error()
		}
		r.Error = &rpcError
		return r
	}

	r.Result = result
	return r
}

// ServeRPC serves the electrum protocol on the given transport.
// This blocks untill the connection is closed
func (e *Electrum) ServeRPC(transport RPCTransport) {
	e.activeConnections.Add(1)

	cl := &Client{
		transport:            transport,
		timer:                time.NewTimer(connectionTimeout),
		serviceMap:           make(map[string]rpcFunc),
		addressSubscriptions: make(map[types.UnlockHash]bool),
	}

	updateChan := make(chan *Update)

	// register the connection
	e.mu.Lock()
	e.connections[cl] = updateChan
	e.mu.Unlock()

	if err := e.registerServerMethods(cl); err != nil {
		e.log.Println("[ERROR] Failed to register server methods:", err)
		e.closeConnection(cl)
	}

	// start read loop
	for {
		select {

		case <-e.stopChan:
			// Server is closing, so close the connection
			e.log.Println("Closing connection to", cl.transport.RemoteAddr(), "due to server shutdown")
			e.closeConnection(cl)
			return

		case <-cl.transport.IsClosed():
			// transport closing, exit
			e.log.Println("Closed connection to", cl.transport.RemoteAddr(), "as transport is closed")
			return

		case update := <-updateChan:
			// handle subscriptions.
			cl.sendUpdate(update)

		case reqs := <-cl.transport.GetRequest():
			// handle request
			cl.resetTimer()

			resp := reqs.NewResponse()

			// Don't block on calls
			go func() {
				// Make sure we don't close the client before this request completes
				cl.wg.Add(1)

				// Ensure all requests in the batch request complete before sending the response
				var bwg sync.WaitGroup
				bwg.Add(len(reqs.requests))

				errChan := make(chan error, len(reqs.requests))
				defer close(errChan)

				for i := range reqs.requests {
					go func(idx int) {
						defer bwg.Done()

						req := reqs.requests[idx]
						result, err := cl.call(req)
						if err == errFatal {
							errChan <- err
							return
						}

						response := newResponse(req.ID, result, err)
						resp.responses[idx] = response
					}(i)
				}
				bwg.Wait()

				select {
				// Check if any call created an error
				case <-errChan:
					// fatal error, close the connection
					cl.wg.Done()
					e.log.Println("Closing connection to", cl.transport.RemoteAddr(), "due to an error")
					e.closeConnection(cl)
					return
				default:
				}

				// Avoid sending nil
				if resp.MustSend() {
					cl.transport.Send(resp)
				}

				cl.wg.Done()
			}()

		case err := <-cl.transport.GetError():
			// handle error

			// Check if the underlying transport is closed. If so, clean up
			if _, ok := err.(errTransportClosed); ok {
				e.log.Println("Cleaning up connection to", cl.transport.RemoteAddr(), "as underlying transport was closed")
				e.closeConnection(cl)
				return
			}

			e.log.Debugln("Client error on connection:", err)
			// If we are here the connection is alive but the request could
			// not be parsed. Send a parse error
			response := &Response{
				Error:   &ErrParse,
				JSONRPC: jsonRPCVersion,
			}
			cl.transport.Send(response)
			cl.resetTimer()

		case <-cl.timer.C:
			// Client didn't sent any request for the past `connectionTimeout`
			// seconds. As such, disconnect the client for being idle
			e.log.Println("Closing connection to", cl.transport.RemoteAddr(), "for being idle")
			e.closeConnection(cl)
			return

		}
	}
}

// closeConnection is a utility mehtod to clean up a connection
func (e *Electrum) closeConnection(cl *Client) error {
	defer e.activeConnections.Done()

	e.log.Debugln("Closing connection to", cl.transport.RemoteAddr())

	// unregister the connections
	e.mu.Lock()
	delete(e.connections, cl)
	e.mu.Unlock()
	// Stop the timer
	if !cl.timer.Stop() {
		// Drain channel if required
		select {
		case <-cl.timer.C:
		default:
		}
	}
	err := cl.transport.Close(&cl.wg)
	if err != nil {
		e.log.Println("[ERROR]: Failed to close connection:", err)
	}
	return err
}
