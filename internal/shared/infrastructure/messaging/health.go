package messaging

import (
	"context"
	"errors"
	amqp "github.com/rabbitmq/amqp091-go"
	"net"
	"os"
	"time"
)

// Inspect topology on a separate, bounded connection; no business messages are published or consumed.
func BrokerHealth(ctx context.Context, service string, receiverReady bool) (map[string]any, error) {
	connection, err := amqp.DialConfig(os.Getenv("RABBITMQ_URL"), amqp.Config{Heartbeat: 5 * time.Second, Dial: func(network, address string) (net.Conn, error) {
		socket, err := (&net.Dialer{Timeout: 1500 * time.Millisecond}).DialContext(ctx, network, address)
		if err == nil {
			deadline := time.Now().Add(2 * time.Second)
			if d, ok := ctx.Deadline(); ok && d.Before(deadline) {
				deadline = d
			}
			_ = socket.SetDeadline(deadline)
		}
		return socket, err
	}})
	if err != nil {
		return nil, err
	}
	defer connection.Close()
	channel, err := connection.Channel()
	if err != nil {
		return nil, err
	}
	if err = channel.ExchangeDeclarePassive("hydrosmart.rpc", "direct", true, false, false, false, nil); err != nil {
		return nil, err
	}
	details := map[string]any{"connection": "UP"}
	if service != "config-service" {
		if err = channel.ExchangeDeclarePassive("hydrosmart.events", "topic", true, false, false, false, nil); err != nil {
			return details, err
		}
		if err = channel.Confirm(false); err != nil {
			return details, err
		}
		details["publisherChannel"] = "READY"
	}
	queue, err := channel.QueueInspect("rpc." + service)
	if err != nil {
		return details, err
	}
	ready := receiverReady && queue.Consumers > 0
	details["rpcReceiver"] = "DOWN"
	if ready {
		details["rpcReceiver"] = "UP"
		return details, nil
	}
	return details, errors.New("receiver unavailable")
}
