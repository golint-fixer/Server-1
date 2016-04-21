package handlers

import (
	"fmt"
	"net/http"
	"sync/atomic"

	"github.com/CodeCollaborate/Server/modules/config"
	"github.com/CodeCollaborate/Server/modules/datahandling"
	"github.com/CodeCollaborate/Server/modules/rabbitmq"
	"github.com/CodeCollaborate/Server/utils"
	"github.com/gorilla/websocket"
)

/**
 * WSManager handles all WebSocket upgrade requests.
 */

// Counter for unique ID of WebSockets Connections. Unique to hostname.
var atomicIDCounter uint64

// Define WebSocket Upgrader that ignores origin; there is never going to be a referral source.
var upgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool {
		return true
	},
}

// NewWSConn accepts a HTTP Upgrade request, creating a new websocket connection.
// Once a WebSocket connection is created, will setup the Receiving and Sending routines,
// then
func NewWSConn(responseWriter http.ResponseWriter, request *http.Request) {
	// Receive and upgrade request
	if request.URL.Path != "/ws/" {
		http.Error(responseWriter, "Not found", 404)
		return
	}
	if request.Method != "GET" {
		http.Error(responseWriter, "Method not allowed", 405)
		return
	}
	wsConn, err := upgrader.Upgrade(responseWriter, request, nil)
	if err != nil {
		fmt.Printf("Failed to upgrade connection: %s\n", err)
		return
	}
	defer wsConn.Close()

	// Generate unique ID for this websocket
	wsID := atomic.AddUint64(&atomicIDCounter, 1)

	// Run WSSendingHandler in a separate GoRoutine
	sendingRoutineControl := utils.NewControl()
	go WSSendingRoutine(wsID, wsConn, sendingRoutineControl)

	for {
		messageType, message, err := wsConn.ReadMessage()
		if err != nil {
			fmt.Println("WebSocket failed to read message, exiting handler")
			sendingRoutineControl.Exit <- true
			break
		}
		dh := datahandling.DataHandler{}
		go dh.Handle(wsID, messageType, message)
	}
}

// WSSendingRoutine receives messages from the RabbitMq subscriber and passes them to the WebSocket.
func WSSendingRoutine(wsID uint64, wsConn *websocket.Conn, ctrl *utils.Control) {

	// Don't check for error; if it would have
	config := config.GetConfig()

	err := rabbitmq.RunSubscriber(
		&rabbitmq.AMQPSubCfg{
			ExchangeName: config.ServerConfig.Name,
			QueueID:      wsID,
			Keys:         []string{},
			IsWorkQueue:  false,
			HandleMessageFunc: func(msg rabbitmq.AMQPMessage) error {
				return wsConn.WriteMessage(websocket.TextMessage, msg.Message)
			},
			Control: ctrl,
		},
	)
	if err != nil {
		utils.LogOnError(err, "Failed to subscribe")
		return
	}
}