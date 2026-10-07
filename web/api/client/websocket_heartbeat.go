package client

import (
	"time"

	"github.com/Sake-My/Komari-Nova/web/connection"
	"github.com/gorilla/websocket"
)

const (
	v2WebSocketReadWait  = 75 * time.Second
	v2WebSocketWriteWait = 10 * time.Second
)

func setWebSocketPingHandler(conn *connection.SafeConn, readWait, writeWait time.Duration) {
	conn.SetPingHandler(func(data string) error {
		if err := conn.SetReadDeadline(time.Now().Add(readWait)); err != nil {
			return err
		}
		return conn.WriteControl(
			websocket.PongMessage,
			[]byte(data),
			time.Now().Add(writeWait),
		)
	})
}
