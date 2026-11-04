// Package messaging carries service requests over RabbitMQ; HTTP is only a local adapter.
package messaging

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	amqp "github.com/rabbitmq/amqp091-go"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync/atomic"
	"time"
)

type Request struct {
	Method   string            `json:"method"`
	Path     string            `json:"path"`
	Headers  map[string]string `json:"headers"`
	Body     string            `json:"body"`
	Deadline int64             `json:"deadline"`
}
type Response struct {
	Status  int               `json:"status"`
	Headers map[string]string `json:"headers"`
	Body    string            `json:"body"`
}
type Transport struct{ URL string }

func NewTransport() *Transport { return &Transport{URL: os.Getenv("RABBITMQ_URL")} }
func (t *Transport) RoundTrip(r *http.Request) (*http.Response, error) {
	service := r.URL.Hostname()
	if !allowedService(service) {
		return nil, errors.New("unknown broker destination")
	}
	body := []byte{}
	if r.Body != nil {
		var err error
		body, err = io.ReadAll(io.LimitReader(r.Body, 12<<20))
		if err != nil {
			return nil, err
		}
	}
	headers := map[string]string{}
	for _, key := range []string{"Authorization", "Content-Type", "X-Service-Token"} {
		if value := r.Header.Get(key); value != "" {
			headers[key] = value
		}
	}
	deadline := time.Now().Add(12 * time.Second)
	if d, ok := r.Context().Deadline(); ok {
		deadline = d
	}
	request := Request{Method: r.Method, Path: r.URL.RequestURI(), Headers: headers, Body: base64.StdEncoding.EncodeToString(body), Deadline: deadline.UnixMilli()}
	result, err := Call(r.Context(), t.URL, service, request)
	if err != nil {
		return nil, err
	}
	raw, err := base64.StdEncoding.DecodeString(result.Body)
	if err != nil {
		return nil, err
	}
	res := &http.Response{StatusCode: result.Status, Status: fmt.Sprintf("%d %s", result.Status, http.StatusText(result.Status)), Header: http.Header{}, Body: io.NopCloser(bytes.NewReader(raw)), Request: r}
	for k, v := range result.Headers {
		res.Header.Set(k, v)
	}
	return res, nil
}
func allowedService(name string) bool {
	switch name {
	case "identity", "properties", "telemetry", "analytics", "reports", "notifications", "billing", "config-service":
		return true
	}
	return false
}
func dial(address string) (*amqp.Connection, error) {
	return amqp.DialConfig(address, amqp.Config{Heartbeat: 30 * time.Second, Dial: amqp.DefaultDial(4 * time.Second)})
}
func Call(ctx context.Context, address, service string, request Request) (Response, error) {
	var result Response
	if !allowedService(service) || address == "" {
		return result, errors.New("broker configuration unavailable")
	}
	connection, err := dial(address)
	if err != nil {
		return result, err
	}
	defer connection.Close()
	channel, err := connection.Channel()
	if err != nil {
		return result, err
	}
	defer channel.Close()
	queue, err := channel.QueueDeclare("", false, true, true, false, nil)
	if err != nil {
		return result, err
	}
	replies, err := channel.Consume(queue.Name, "", true, true, false, false, nil)
	if err != nil {
		return result, err
	}
	id := fmt.Sprintf("%s-%d", service, time.Now().UnixNano())
	data, err := json.Marshal(request)
	if err != nil {
		return result, err
	}
	err = channel.PublishWithContext(ctx, "hydrosmart.rpc", service, true, false, amqp.Publishing{ContentType: "application/json", CorrelationId: id, ReplyTo: queue.Name, Expiration: "15000", DeliveryMode: 2, Body: data})
	if err != nil {
		return result, err
	}
	timer := time.NewTimer(12 * time.Second)
	defer timer.Stop()
	for {
		select {
		case reply, ok := <-replies:
			if !ok {
				return result, errors.New("broker reply channel closed")
			}
			if reply.CorrelationId == id {
				if len(reply.Body) > 16<<20 {
					return result, errors.New("broker reply too large")
				}
				err = json.Unmarshal(reply.Body, &result)
				return result, err
			}
		case <-ctx.Done():
			return result, ctx.Err()
		case <-timer.C:
			return result, errors.New("broker request timeout")
		}
	}
}

type writer struct {
	header http.Header
	body   bytes.Buffer
	status int
}

func (w *writer) Header() http.Header { return w.header }
func (w *writer) WriteHeader(status int) {
	if w.status == 0 {
		w.status = status
	}
}
func (w *writer) Write(data []byte) (int, error) {
	if w.status == 0 {
		w.status = 200
	}
	if w.body.Len()+len(data) > 12<<20 {
		return 0, errors.New("response too large")
	}
	return w.body.Write(data)
}

type Server struct{ ready atomic.Bool }

func (s *Server) Ready() bool { return s.ready.Load() }
func StartServer(ctx context.Context, service string, handler http.Handler) *Server {
	state := &Server{}
	go func() {
		for ctx.Err() == nil {
			connection, err := dial(os.Getenv("RABBITMQ_URL"))
			if err == nil {
				channel, e := connection.Channel()
				err = e
				if err == nil {
					_ = channel.ExchangeDeclare("hydrosmart.rpc", "direct", true, false, false, false, nil)
					queue, e := channel.QueueDeclare("rpc."+service, true, false, false, false, nil)
					err = e
					if err == nil {
						err = channel.QueueBind(queue.Name, service, "hydrosmart.rpc", false, nil)
					}
					if err == nil {
						err = channel.Qos(8, 0, false)
					}
					if err == nil {
						deliveries, e := channel.Consume(queue.Name, service+".rpc", false, false, false, false, nil)
						err = e
						if err == nil {
							state.ready.Store(true)
							for ctx.Err() == nil {
								select {
								case delivery, ok := <-deliveries:
									if !ok {
										err = errors.New("broker disconnected")
										goto done
									}
									var request Request
									if len(delivery.Body) > 16<<20 || json.Unmarshal(delivery.Body, &request) != nil || delivery.ReplyTo == "" || delivery.CorrelationId == "" {
										_ = delivery.Reject(false)
										continue
									}
									result := Response{Status: 400, Headers: map[string]string{"Content-Type": "application/json"}, Body: base64.StdEncoding.EncodeToString([]byte("{\"error\":\"invalid_broker_request\"}"))}
									parsed, parseErr := url.ParseRequestURI(request.Path)
									body, decodeErr := base64.StdEncoding.DecodeString(request.Body)
									if request.Deadline < time.Now().UnixMilli() {
										result.Status = 504
									} else if parseErr == nil && decodeErr == nil && !parsed.IsAbs() && strings.HasPrefix(request.Path, "/") && !strings.HasPrefix(request.Path, "//") {
										local, _ := http.NewRequestWithContext(ctx, request.Method, "http://localhost"+request.Path, bytes.NewReader(body))
										for key, value := range request.Headers {
											if key == "Authorization" || key == "X-Service-Token" || key == "Content-Type" {
												local.Header.Set(key, value)
											}
										}
										w := &writer{header: http.Header{}}
										handler.ServeHTTP(w, local)
										if w.status == 0 {
											w.status = 200
										}
										result.Status = w.status
										result.Headers = map[string]string{"Content-Type": w.header.Get("Content-Type")}
										result.Body = base64.StdEncoding.EncodeToString(w.body.Bytes())
									}
									response, _ := json.Marshal(result)
									if e := channel.PublishWithContext(ctx, "", delivery.ReplyTo, false, false, amqp.Publishing{CorrelationId: delivery.CorrelationId, ContentType: "application/json", Body: response}); e != nil {
										err = e
										goto done
									}
									_ = delivery.Ack(false)
								case <-ctx.Done():
									goto done
								}
							}
						}
					}
				}
			done:
				state.ready.Store(false)
				_ = connection.Close()
			}
			select {
			case <-ctx.Done():
				return
			case <-time.After(2 * time.Second):
			}
		}
	}()
	return state
}
