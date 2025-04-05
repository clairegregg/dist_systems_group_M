package main

import (
	"encoding/json"
	"fmt"
	"log"
	"math"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/Cormuckle/dist_systems_group_M/chunk_server/kafka"
	"github.com/Cormuckle/dist_systems_group_M/chunk_server/playerstate"
	"github.com/Cormuckle/dist_systems_group_M/chunk_server/websocket"
	"github.com/gin-contrib/cors"
	"github.com/gin-gonic/gin"
)

var (
	kafkaProducer   *kafka.Producer
	kafkaConsumer   *kafka.Consumer // main consumer for other messages
	db              = make(map[string]string)
	chunkID         string
	chunkTopic      string
	centralTopic    = "chunk_to_central"
	broadcastTopic  = "central_to_chunk_broadcast"
	localMap        [][]string // holds the 2D map in memory
	chunkCoordinate string     // the raw coordinate for this chunk server (e.g. "0,0")
	// We'll store the Kafka broker address in a global variable for use in requestMap.
	kafkaBroker string
	chunkKey    string
)

var Maps = [][][]string{
	{
		{"1", "1", "1", "1", "1", "1", "1", "1", "0", "1", "1", "1", "1", "1", "1", "1", "1"},
		{"1", "0", "0", "0", "0", "0", "0", "0", "0", "0", "0", "0", "0", "0", "0", "0", "1"},
		{"1", "0", "1", "1", "0", "1", "1", "0", "1", "0", "1", "1", "0", "1", "1", "0", "1"},
		{"1", "0", "0", "0", "0", "0", "0", "0", "1", "0", "0", "0", "0", "0", "0", "0", "1"},
		{"1", "0", "1", "1", "0", "1", "0", "1", "1", "1", "0", "1", "0", "1", "1", "0", "1"},
		{"1", "0", "0", "0", "0", "1", "0", "0", "0", "0", "0", "1", "0", "0", "0", "0", "1"},
		{"1", "1", "1", "1", "0", "1", "1", "1", "0", "1", "1", "1", "0", "1", "1", "1", "1"},
		{"1", "1", "1", "1", "0", "1", "0", "0", "0", "0", "0", "1", "0", "1", "1", "1", "1"},
		{"0", "0", "0", "0", "0", "1", "0", "1", "1", "1", "0", "1", "0", "0", "0", "0", "0"},
		{"1", "1", "1", "1", "0", "0", "0", "1", "1", "1", "0", "0", "0", "1", "1", "1", "1"},
		{"1", "1", "1", "1", "0", "1", "0", "0", "0", "0", "0", "1", "0", "1", "1", "1", "1"},
		{"1", "0", "0", "0", "0", "1", "0", "1", "1", "1", "0", "1", "0", "0", "0", "0", "1"},
		{"1", "0", "1", "1", "0", "1", "0", "0", "0", "0", "0", "1", "0", "1", "1", "0", "1"},
		{"1", "0", "1", "1", "0", "0", "0", "1", "1", "1", "0", "0", "0", "1", "1", "0", "1"},
		{"1", "0", "1", "1", "0", "1", "0", "0", "1", "0", "0", "1", "0", "1", "1", "0", "1"},
		{"1", "0", "0", "0", "0", "1", "1", "0", "0", "0", "1", "1", "0", "0", "0", "0", "1"},
		{"1", "1", "1", "1", "1", "1", "1", "1", "0", "1", "1", "1", "1", "1", "1", "1", "1"},
	},
	{
		{"1", "1", "1", "1", "1", "1", "1", "1", "0", "1", "1", "1", "1", "1", "1", "1", "1"},
		{"1", "1", "1", "1", "0", "0", "0", "0", "0", "0", "0", "0", "0", "1", "1", "1", "1"},
		{"1", "1", "0", "0", "0", "1", "1", "1", "1", "1", "1", "1", "0", "0", "0", "1", "1"},
		{"1", "1", "0", "1", "0", "1", "1", "1", "1", "1", "1", "1", "0", "1", "0", "1", "1"},
		{"1", "0", "0", "1", "0", "0", "0", "0", "0", "0", "0", "0", "0", "1", "0", "0", "1"},
		{"1", "0", "1", "1", "0", "1", "1", "0", "1", "0", "1", "1", "0", "1", "1", "0", "1"},
		{"1", "0", "0", "0", "0", "1", "0", "0", "1", "0", "0", "1", "0", "0", "0", "0", "1"},
		{"1", "0", "1", "1", "0", "1", "0", "1", "1", "1", "0", "1", "0", "1", "1", "0", "1"},
		{"0", "0", "1", "1", "0", "0", "0", "0", "0", "0", "0", "0", "0", "1", "1", "0", "0"},
		{"1", "0", "1", "1", "0", "1", "0", "1", "1", "1", "0", "1", "0", "1", "1", "0", "1"},
		{"1", "0", "0", "0", "0", "1", "0", "0", "1", "0", "0", "1", "0", "0", "0", "0", "1"},
		{"1", "0", "1", "1", "0", "1", "1", "0", "1", "0", "1", "1", "0", "1", "1", "0", "1"},
		{"1", "0", "0", "1", "0", "0", "0", "0", "0", "0", "0", "0", "0", "1", "0", "0", "1"},
		{"1", "1", "0", "1", "0", "1", "1", "1", "1", "1", "1", "1", "0", "1", "0", "1", "1"},
		{"1", "1", "0", "0", "0", "1", "1", "1", "1", "1", "1", "1", "0", "0", "0", "1", "1"},
		{"1", "1", "1", "1", "0", "0", "0", "0", "0", "0", "0", "0", "0", "1", "1", "1", "1"},
		{"1", "1", "1", "1", "1", "1", "1", "1", "0", "1", "1", "1", "1", "1", "1", "1", "1"},
	},
	{
		{"1", "1", "1", "1", "1", "1", "1", "1", "0", "1", "1", "1", "1", "1", "1", "1", "1"},
		{"1", "0", "0", "0", "0", "0", "0", "1", "0", "1", "0", "0", "0", "0", "0", "0", "1"},
		{"1", "0", "1", "1", "1", "1", "0", "1", "0", "1", "0", "1", "1", "1", "1", "0", "1"},
		{"1", "0", "1", "1", "1", "1", "0", "1", "0", "1", "0", "1", "1", "1", "1", "0", "1"},
		{"1", "0", "0", "0", "0", "0", "0", "0", "0", "0", "0", "0", "0", "0", "0", "0", "1"},
		{"1", "0", "1", "0", "1", "1", "1", "0", "1", "0", "1", "1", "1", "0", "1", "0", "1"},
		{"1", "0", "1", "0", "1", "1", "1", "0", "1", "0", "1", "1", "1", "0", "1", "0", "1"},
		{"1", "0", "1", "0", "1", "1", "1", "0", "1", "0", "1", "1", "1", "0", "1", "0", "1"},
		{"0", "0", "0", "0", "0", "0", "0", "0", "1", "0", "0", "0", "0", "0", "0", "0", "0"},
		{"1", "0", "1", "1", "1", "1", "0", "1", "1", "1", "0", "1", "1", "1", "1", "0", "1"},
		{"1", "0", "0", "0", "0", "0", "0", "0", "0", "0", "0", "0", "0", "0", "0", "0", "1"},
		{"1", "0", "1", "0", "1", "1", "0", "1", "0", "1", "0", "1", "1", "0", "1", "0", "1"},
		{"1", "0", "1", "0", "1", "1", "0", "1", "0", "1", "0", "1", "1", "0", "1", "0", "1"},
		{"1", "0", "1", "0", "1", "1", "0", "0", "0", "0", "0", "1", "1", "0", "1", "0", "1"},
		{"1", "0", "1", "0", "1", "1", "0", "1", "0", "1", "0", "1", "1", "0", "1", "0", "1"},
		{"1", "0", "0", "0", "0", "0", "0", "1", "0", "1", "0", "0", "0", "0", "0", "0", "1"},
		{"1", "1", "1", "1", "1", "1", "1", "1", "0", "1", "1", "1", "1", "1", "1", "1", "1"},
	},
	{
		{"1", "1", "1", "1", "1", "1", "1", "1", "0", "1", "1", "1", "1", "1", "1", "1", "1"},
		{"1", "1", "1", "1", "0", "0", "0", "1", "0", "1", "0", "0", "0", "1", "1", "1", "1"},
		{"1", "0", "0", "0", "0", "1", "0", "0", "0", "0", "0", "1", "0", "0", "0", "0", "1"},
		{"1", "0", "1", "1", "0", "1", "1", "0", "1", "0", "1", "1", "0", "1", "1", "0", "1"},
		{"1", "0", "1", "1", "0", "1", "1", "0", "1", "0", "1", "1", "0", "1", "1", "0", "1"},
		{"1", "0", "1", "1", "0", "1", "0", "0", "1", "0", "0", "1", "0", "1", "1", "0", "1"},
		{"1", "0", "0", "0", "0", "0", "0", "1", "1", "1", "0", "0", "0", "0", "0", "0", "1"},
		{"1", "1", "0", "1", "1", "1", "0", "1", "1", "1", "0", "1", "1", "1", "0", "1", "1"},
		{"0", "0", "0", "0", "0", "0", "0", "1", "1", "1", "0", "0", "0", "0", "0", "0", "0"},
		{"1", "1", "0", "1", "1", "1", "0", "1", "1", "1", "0", "1", "1", "1", "0", "1", "1"},
		{"1", "0", "0", "0", "0", "0", "0", "1", "1", "1", "0", "0", "0", "0", "0", "0", "1"},
		{"1", "0", "1", "1", "0", "1", "0", "0", "1", "0", "0", "1", "0", "1", "1", "0", "1"},
		{"1", "0", "1", "1", "0", "1", "1", "0", "1", "0", "1", "1", "0", "1", "1", "0", "1"},
		{"1", "0", "1", "1", "0", "1", "1", "0", "1", "0", "1", "1", "0", "1", "1", "0", "1"},
		{"1", "0", "0", "0", "0", "1", "0", "0", "0", "0", "0", "1", "0", "0", "0", "0", "1"},
		{"1", "1", "1", "1", "0", "0", "0", "1", "0", "1", "0", "0", "0", "1", "1", "1", "1"},
		{"1", "1", "1", "1", "1", "1", "1", "1", "0", "1", "1", "1", "1", "1", "1", "1", "1"},
	},
	{
		{"1", "1", "1", "1", "1", "1", "1", "1", "0", "1", "1", "1", "1", "1", "1", "1", "1"},
		{"1", "0", "0", "0", "1", "1", "1", "1", "0", "1", "1", "1", "1", "0", "0", "0", "1"},
		{"1", "0", "1", "0", "0", "0", "0", "0", "0", "0", "0", "0", "0", "0", "1", "0", "1"},
		{"1", "0", "1", "0", "1", "1", "0", "1", "1", "1", "0", "1", "1", "0", "1", "0", "1"},
		{"1", "0", "1", "0", "1", "1", "0", "1", "1", "1", "0", "1", "1", "0", "1", "0", "1"},
		{"1", "0", "0", "0", "0", "0", "0", "0", "1", "0", "0", "0", "0", "0", "0", "0", "1"},
		{"1", "0", "1", "0", "1", "1", "1", "0", "0", "0", "1", "1", "1", "0", "1", "0", "1"},
		{"1", "0", "1", "0", "0", "1", "0", "0", "1", "0", "0", "1", "0", "0", "1", "0", "1"},
		{"0", "0", "1", "1", "0", "0", "0", "1", "1", "1", "0", "0", "0", "1", "1", "0", "0"},
		{"1", "0", "1", "0", "0", "1", "0", "0", "1", "0", "0", "1", "0", "0", "1", "0", "1"},
		{"1", "0", "1", "0", "1", "1", "1", "0", "0", "0", "1", "1", "1", "0", "1", "0", "1"},
		{"1", "0", "0", "0", "0", "0", "0", "0", "1", "0", "0", "0", "0", "0", "0", "0", "1"},
		{"1", "0", "1", "0", "1", "1", "0", "1", "1", "1", "0", "1", "1", "0", "1", "0", "1"},
		{"1", "0", "1", "0", "1", "1", "0", "1", "1", "1", "0", "1", "1", "0", "1", "0", "1"},
		{"1", "0", "1", "0", "0", "0", "0", "0", "0", "0", "0", "0", "0", "0", "1", "0", "1"},
		{"1", "0", "0", "0", "1", "1", "1", "1", "0", "1", "1", "1", "1", "0", "0", "0", "1"},
		{"1", "1", "1", "1", "1", "1", "1", "1", "0", "1", "1", "1", "1", "1", "1", "1", "1"},
	},
	{
		{"1", "1", "1", "1", "1", "1", "1", "1", "0", "1", "1", "1", "1", "1", "1", "1", "1"},
		{"1", "0", "0", "0", "0", "0", "0", "1", "0", "1", "0", "0", "0", "0", "0", "0", "1"},
		{"1", "0", "1", "0", "1", "1", "0", "0", "0", "0", "0", "1", "1", "0", "1", "0", "1"},
		{"1", "0", "1", "0", "1", "1", "1", "0", "1", "0", "1", "1", "1", "0", "1", "0", "1"},
		{"1", "0", "1", "0", "0", "1", "0", "0", "1", "0", "0", "1", "0", "0", "1", "0", "1"},
		{"1", "0", "1", "1", "0", "1", "0", "1", "1", "1", "0", "1", "0", "1", "1", "0", "1"},
		{"1", "0", "0", "1", "0", "0", "0", "0", "0", "0", "0", "0", "0", "1", "0", "0", "1"},
		{"1", "1", "0", "0", "0", "1", "0", "1", "0", "1", "0", "1", "0", "0", "0", "1", "1"},
		{"0", "0", "0", "1", "1", "1", "0", "0", "0", "0", "0", "1", "1", "1", "0", "0", "0"},
		{"1", "1", "0", "0", "0", "1", "0", "1", "0", "1", "0", "1", "0", "0", "0", "1", "1"},
		{"1", "1", "0", "1", "0", "0", "0", "0", "0", "0", "0", "0", "0", "1", "0", "1", "1"},
		{"1", "0", "0", "0", "0", "1", "0", "1", "1", "1", "0", "1", "0", "0", "0", "0", "1"},
		{"1", "0", "1", "1", "0", "1", "0", "0", "1", "0", "0", "1", "0", "1", "1", "0", "1"},
		{"1", "0", "1", "1", "0", "1", "1", "0", "1", "0", "1", "1", "0", "1", "1", "0", "1"},
		{"1", "0", "0", "1", "0", "1", "1", "0", "1", "0", "1", "1", "0", "1", "0", "0", "1"},
		{"1", "1", "0", "0", "0", "0", "0", "0", "0", "0", "0", "0", "0", "0", "0", "1", "1"},
		{"1", "1", "1", "1", "1", "1", "1", "1", "0", "1", "1", "1", "1", "1", "1", "1", "1"},
	},
	{
		{"1", "1", "1", "1", "1", "1", "1", "1", "0", "1", "1", "1", "1", "1", "1", "1", "1"},
		{"1", "0", "0", "0", "0", "0", "1", "1", "0", "1", "1", "0", "0", "0", "0", "0", "1"},
		{"1", "0", "1", "1", "1", "0", "0", "0", "0", "0", "0", "0", "1", "1", "1", "0", "1"},
		{"1", "0", "1", "1", "0", "0", "1", "0", "1", "0", "1", "0", "0", "1", "1", "0", "1"},
		{"1", "0", "1", "1", "0", "1", "1", "0", "1", "0", "1", "1", "0", "1", "1", "0", "1"},
		{"1", "0", "0", "1", "0", "1", "1", "0", "1", "0", "1", "1", "0", "1", "0", "0", "1"},
		{"1", "1", "0", "0", "0", "0", "0", "0", "0", "0", "0", "0", "0", "0", "0", "1", "1"},
		{"1", "1", "1", "0", "1", "1", "0", "1", "1", "1", "0", "1", "1", "0", "1", "1", "1"},
		{"0", "0", "0", "0", "1", "1", "0", "1", "1", "1", "0", "1", "1", "0", "0", "0", "0"},
		{"1", "1", "1", "0", "1", "0", "0", "0", "0", "0", "0", "0", "1", "0", "1", "1", "1"},
		{"1", "1", "1", "0", "1", "0", "1", "1", "1", "1", "1", "0", "1", "0", "1", "1", "1"},
		{"1", "0", "0", "0", "0", "0", "0", "0", "0", "0", "0", "0", "0", "0", "0", "0", "1"},
		{"1", "0", "1", "1", "1", "1", "0", "1", "1", "1", "0", "1", "1", "1", "1", "0", "1"},
		{"1", "0", "0", "0", "0", "0", "0", "1", "1", "1", "0", "0", "0", "0", "0", "0", "1"},
		{"1", "0", "1", "1", "1", "1", "0", "0", "0", "0", "0", "1", "1", "1", "1", "0", "1"},
		{"1", "0", "0", "0", "0", "0", "0", "1", "0", "1", "0", "0", "0", "0", "0", "0", "1"},
		{"1", "1", "1", "1", "1", "1", "1", "1", "0", "1", "1", "1", "1", "1", "1", "1", "1"},
	},
	{
		{"1", "1", "1", "1", "1", "1", "1", "1", "0", "1", "1", "1", "1", "1", "1", "1", "1"},
		{"1", "0", "0", "0", "0", "0", "0", "0", "0", "0", "0", "0", "0", "0", "0", "0", "1"},
		{"1", "0", "1", "0", "1", "1", "1", "1", "0", "1", "1", "1", "1", "0", "1", "0", "1"},
		{"1", "0", "1", "0", "0", "0", "0", "0", "0", "0", "0", "0", "0", "0", "1", "0", "1"},
		{"1", "0", "1", "1", "0", "1", "1", "1", "0", "1", "1", "1", "0", "1", "1", "0", "1"},
		{"1", "0", "0", "0", "0", "0", "1", "1", "0", "1", "1", "0", "0", "0", "0", "0", "1"},
		{"1", "0", "1", "1", "1", "0", "0", "1", "0", "1", "0", "0", "1", "1", "1", "0", "1"},
		{"1", "0", "1", "1", "1", "1", "0", "1", "0", "1", "0", "1", "1", "1", "1", "0", "1"},
		{"0", "0", "0", "0", "0", "0", "0", "0", "0", "0", "0", "0", "0", "0", "0", "0", "0"},
		{"1", "0", "1", "1", "1", "1", "0", "1", "0", "1", "0", "1", "1", "1", "1", "0", "1"},
		{"1", "0", "1", "1", "1", "0", "0", "1", "0", "1", "0", "0", "1", "1", "1", "0", "1"},
		{"1", "0", "0", "0", "0", "0", "1", "1", "0", "1", "1", "0", "0", "0", "0", "0", "1"},
		{"1", "0", "1", "1", "0", "1", "1", "1", "0", "1", "1", "1", "0", "1", "1", "0", "1"},
		{"1", "0", "1", "0", "0", "0", "0", "0", "0", "0", "0", "0", "0", "0", "1", "0", "1"},
		{"1", "0", "1", "0", "1", "1", "1", "1", "0", "1", "1", "1", "1", "0", "1", "0", "1"},
		{"1", "0", "0", "0", "0", "0", "0", "0", "0", "0", "0", "0", "0", "0", "0", "0", "1"},
		{"1", "1", "1", "1", "1", "1", "1", "1", "0", "1", "1", "1", "1", "1", "1", "1", "1"},
	},
	{
		{"1", "1", "1", "1", "1", "1", "1", "1", "0", "1", "1", "1", "1", "1", "1", "1", "1"},
		{"1", "0", "0", "0", "0", "0", "0", "0", "0", "0", "0", "0", "0", "0", "0", "0", "1"},
		{"1", "0", "1", "1", "1", "1", "1", "1", "0", "1", "1", "1", "1", "1", "1", "0", "1"},
		{"1", "0", "1", "0", "0", "0", "0", "0", "0", "0", "0", "0", "0", "0", "1", "0", "1"},
		{"1", "0", "1", "0", "1", "1", "1", "1", "0", "1", "1", "1", "1", "0", "1", "0", "1"},
		{"1", "0", "1", "0", "1", "0", "0", "0", "0", "0", "0", "0", "1", "0", "1", "0", "1"},
		{"1", "0", "1", "0", "1", "0", "1", "1", "0", "1", "1", "0", "1", "0", "1", "0", "1"},
		{"1", "0", "1", "0", "1", "0", "1", "0", "0", "0", "1", "0", "1", "0", "1", "0", "1"},
		{"0", "0", "0", "0", "0", "0", "0", "0", "1", "0", "0", "0", "0", "0", "0", "0", "0"},
		{"1", "0", "1", "0", "1", "0", "1", "0", "0", "0", "1", "0", "1", "0", "1", "0", "1"},
		{"1", "0", "1", "0", "1", "0", "1", "1", "0", "1", "1", "0", "1", "0", "1", "0", "1"},
		{"1", "0", "1", "0", "1", "0", "0", "0", "0", "0", "0", "0", "1", "0", "1", "0", "1"},
		{"1", "0", "1", "0", "1", "1", "1", "1", "0", "1", "1", "1", "1", "0", "1", "0", "1"},
		{"1", "0", "1", "0", "0", "0", "0", "0", "0", "0", "0", "0", "0", "0", "1", "0", "1"},
		{"1", "0", "1", "1", "1", "1", "1", "1", "0", "1", "1", "1", "1", "1", "1", "0", "1"},
		{"1", "0", "0", "0", "0", "0", "0", "0", "0", "0", "0", "0", "0", "0", "0", "0", "1"},
		{"1", "1", "1", "1", "1", "1", "1", "1", "0", "1", "1", "1", "1", "1", "1", "1", "1"},
	},
	{
		{"1", "1", "1", "1", "1", "1", "1", "1", "0", "1", "1", "1", "1", "1", "1", "1", "1"},
		{"1", "1", "1", "1", "0", "0", "0", "1", "0", "1", "0", "0", "0", "0", "0", "0", "1"},
		{"1", "0", "0", "0", "0", "1", "0", "1", "0", "0", "0", "1", "0", "1", "1", "0", "1"},
		{"1", "0", "1", "0", "1", "1", "0", "0", "0", "1", "0", "0", "0", "0", "0", "0", "1"},
		{"1", "0", "0", "0", "0", "1", "0", "1", "0", "0", "0", "1", "0", "1", "1", "0", "1"},
		{"1", "1", "0", "1", "0", "0", "0", "0", "0", "1", "0", "0", "0", "0", "0", "0", "1"},
		{"1", "1", "0", "0", "0", "1", "0", "1", "0", "0", "0", "1", "0", "1", "0", "1", "1"},
		{"1", "1", "0", "1", "1", "1", "0", "1", "0", "1", "0", "1", "0", "1", "0", "1", "1"},
		{"0", "0", "0", "0", "0", "0", "0", "0", "0", "0", "0", "0", "0", "0", "0", "0", "0"},
		{"1", "1", "1", "0", "1", "0", "1", "1", "0", "1", "0", "1", "0", "1", "0", "1", "1"},
		{"1", "1", "0", "0", "0", "0", "0", "0", "0", "1", "0", "0", "0", "0", "0", "0", "1"},
		{"1", "1", "0", "1", "0", "1", "0", "1", "0", "0", "0", "1", "1", "0", "1", "0", "1"},
		{"1", "1", "0", "0", "0", "1", "0", "0", "0", "1", "0", "0", "0", "0", "0", "0", "1"},
		{"1", "0", "0", "1", "0", "0", "0", "1", "0", "0", "0", "1", "1", "0", "1", "0", "1"},
		{"1", "0", "1", "1", "1", "1", "0", "0", "0", "1", "0", "1", "1", "0", "1", "0", "1"},
		{"1", "0", "0", "0", "0", "0", "0", "1", "0", "1", "0", "0", "0", "0", "0", "0", "1"},
		{"1", "1", "1", "1", "1", "1", "1", "1", "0", "1", "1", "1", "1", "1", "1", "1", "1"},
	},
	{
		{"1", "1", "1", "1", "1", "1", "1", "1", "0", "1", "1", "1", "1", "1", "1", "1", "1"},
		{"1", "0", "0", "0", "0", "1", "0", "0", "0", "0", "0", "1", "0", "0", "0", "0", "1"},
		{"1", "0", "1", "1", "0", "0", "0", "1", "0", "1", "0", "0", "0", "1", "1", "0", "1"},
		{"1", "0", "0", "1", "1", "0", "1", "1", "0", "1", "1", "0", "1", "1", "0", "0", "1"},
		{"1", "1", "0", "0", "0", "0", "0", "0", "0", "0", "0", "0", "0", "0", "0", "1", "1"},
		{"1", "0", "0", "1", "0", "1", "0", "1", "1", "1", "0", "1", "0", "1", "0", "0", "1"},
		{"1", "0", "1", "1", "0", "0", "0", "0", "0", "0", "0", "0", "0", "1", "1", "0", "1"},
		{"1", "0", "1", "0", "0", "1", "0", "1", "1", "1", "0", "1", "0", "0", "1", "0", "1"},
		{"0", "0", "0", "0", "1", "1", "0", "1", "1", "1", "0", "1", "1", "0", "0", "0", "0"},
		{"1", "0", "1", "0", "0", "1", "0", "1", "1", "1", "0", "1", "0", "0", "1", "0", "1"},
		{"1", "0", "1", "1", "0", "0", "0", "0", "0", "0", "0", "0", "0", "1", "1", "0", "1"},
		{"1", "0", "0", "1", "0", "1", "0", "1", "1", "1", "0", "1", "0", "1", "0", "0", "1"},
		{"1", "1", "0", "0", "0", "0", "0", "0", "0", "0", "0", "0", "0", "0", "0", "1", "1"},
		{"1", "0", "0", "1", "1", "0", "1", "1", "0", "1", "1", "0", "1", "1", "0", "0", "1"},
		{"1", "0", "1", "1", "0", "0", "0", "1", "0", "1", "0", "0", "0", "1", "1", "0", "1"},
		{"1", "0", "0", "0", "0", "1", "0", "0", "0", "0", "0", "1", "0", "0", "0", "0", "1"},
		{"1", "1", "1", "1", "1", "1", "1", "1", "0", "1", "1", "1", "1", "1", "1", "1", "1"},
	},
	{
		{"1", "1", "1", "1", "1", "1", "1", "1", "0", "1", "1", "1", "1", "1", "1", "1", "1"},
		{"1", "0", "0", "0", "0", "0", "0", "0", "0", "0", "0", "0", "0", "0", "0", "0", "1"},
		{"1", "0", "1", "1", "1", "1", "1", "0", "1", "0", "1", "1", "1", "1", "1", "0", "1"},
		{"1", "0", "1", "1", "1", "0", "0", "0", "0", "0", "0", "0", "1", "1", "1", "0", "1"},
		{"1", "0", "1", "0", "0", "0", "1", "0", "1", "0", "1", "0", "0", "0", "1", "0", "1"},
		{"1", "0", "0", "0", "1", "0", "0", "0", "1", "0", "0", "0", "1", "0", "0", "0", "1"},
		{"1", "0", "1", "0", "1", "0", "1", "0", "1", "0", "1", "0", "1", "0", "1", "0", "1"},
		{"1", "0", "1", "0", "0", "0", "1", "0", "0", "0", "1", "0", "0", "0", "1", "0", "1"},
		{"0", "0", "1", "0", "1", "0", "1", "0", "1", "0", "1", "0", "1", "0", "1", "0", "0"},
		{"1", "0", "1", "0", "0", "0", "1", "0", "0", "0", "1", "0", "0", "0", "1", "0", "1"},
		{"1", "0", "1", "0", "1", "0", "1", "0", "1", "0", "1", "0", "1", "0", "1", "0", "1"},
		{"1", "0", "0", "0", "1", "0", "0", "0", "1", "0", "0", "0", "1", "0", "0", "0", "1"},
		{"1", "0", "1", "0", "0", "0", "1", "0", "1", "0", "1", "0", "0", "0", "1", "0", "1"},
		{"1", "0", "1", "1", "1", "0", "0", "0", "0", "0", "0", "0", "1", "1", "1", "0", "1"},
		{"1", "0", "1", "1", "1", "1", "1", "0", "1", "0", "1", "1", "1", "1", "1", "0", "1"},
		{"1", "0", "0", "0", "0", "0", "0", "0", "0", "0", "0", "0", "0", "0", "0", "0", "1"},
		{"1", "1", "1", "1", "1", "1", "1", "1", "0", "1", "1", "1", "1", "1", "1", "1", "1"},
	},
}

func generateChunkID() string {
	hostname, err := os.Hostname()
	if err != nil {
		hostname = fmt.Sprintf("chunk_server_%d", time.Now().Unix())
	}
	return hostname
}

func setupRouter() *gin.Engine {
	r := gin.Default()
	r.Use(cors.Default())
	// Health check endpoint.
	r.GET("/ping", func(c *gin.Context) {
		c.String(http.StatusOK, "pong")
	})
	r.GET("/user/:name", func(c *gin.Context) {
		user := c.Param("name")
		value, ok := db[user]
		if ok {
			c.JSON(http.StatusOK, gin.H{"user": user, "value": value})
		} else {
			c.JSON(http.StatusOK, gin.H{"user": user, "status": "no value"})
		}
	})
	authorized := r.Group("/", gin.BasicAuth(gin.Accounts{
		"foo":  "bar",
		"manu": "123",
	}))
	authorized.POST("admin", func(c *gin.Context) {
		user := c.MustGet(gin.AuthUserKey).(string)
		var jsonData struct {
			Value string `json:"value" binding:"required"`
		}
		if c.Bind(&jsonData) == nil {
			db[user] = jsonData.Value
			c.JSON(http.StatusOK, gin.H{"status": "ok"})
		}
	})
	r.POST("/send", func(c *gin.Context) {
		var req struct {
			Message string `json:"message" binding:"required"`
		}
		if err := c.BindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid request"})
			return
		}
		err := kafkaProducer.SendMessage(centralTopic, fmt.Sprintf("[%s]: %s", chunkID, req.Message))
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"status": "Failed to send"})
			return
		}
		c.JSON(http.StatusOK, gin.H{"status": "Message sent to central server"})
	})
	r.GET("/ws", func(c *gin.Context) {
		websocket.WSHandler(c.Writer, c.Request)
	})
	// Expose the locally stored map.
	r.GET("/getMap", func(c *gin.Context) {
		if localMap == nil {
			c.JSON(http.StatusNotFound, gin.H{"error": "map not loaded"})
		} else {
			c.JSON(http.StatusOK, localMap)
		}
	})
	return r
}

// requestMap uses a dedicated consumer to request and receive the map from the central server.
func waitForCentralServer(centralURL string) {
	for {
		resp, err := http.Get(centralURL + "/ping")
		if err == nil && resp.StatusCode == http.StatusOK {
			log.Printf("Central server is up at %s", centralURL)
			return
		}
		log.Printf("Central server not ready at %s. Retrying in 5 seconds...", centralURL)
		time.Sleep(5 * time.Second)
	}
}

func requestMap() {
	// Optionally, get the central server URL from env (default if not set)
	centralURL := os.Getenv("CENTRAL_SERVER_URL")
	if centralURL == "" {
		centralURL = "http://central_server:8080" // adjust as needed
	}
	// Wait until the central server is available.
	waitForCentralServer(centralURL)

	// Create a dedicated consumer for map responses using a unique group id.
	mapConsumer, err := kafka.NewConsumer(kafkaBroker, chunkID+"_map")
	if err != nil {
		log.Fatalf("Failed to create map consumer: %v", err)
	}
	defer mapConsumer.Close()

	for {
		// Send the raw coordinate (with comma) so the central server can look it up.
		requestMsg := fmt.Sprintf("GET_MAP:%s", chunkCoordinate)
		err := kafkaProducer.SendMessage(centralTopic, requestMsg)
		if err != nil {
			log.Printf("Failed to send map request: %v", err)
		} else {
			log.Printf("Sent map request for coordinate %s", chunkCoordinate)
		}
		// Try to consume a message on our dedicated consumer from our chunk topic.
		msg, err := mapConsumer.ConsumeMessage(chunkTopic)
		if err != nil {
			log.Printf("Error consuming map response: %v", err)
		} else {
			if strings.HasPrefix(msg, "MAP_RESPONSE:") {
				mapJSON := strings.TrimPrefix(msg, "MAP_RESPONSE:")
				var receivedMap [][]string
				err = json.Unmarshal([]byte(mapJSON), &receivedMap)
				if err == nil {
					localMap = receivedMap
					log.Printf("Received map for coordinate %s:", chunkCoordinate)
					for i, row := range localMap {
						log.Printf("Row %d: %v", i, row)
					}
					return
				} else {
					log.Printf("Error unmarshaling map: %v", err)
				}
			}
		}
		time.Sleep(5 * time.Second)
	}
}

func consumeMessages() {
	// Listen on both the unique chunk topic (coordinate-based) and the broadcast topic.
	topics := []string{chunkTopic, broadcastTopic}
	log.Printf("Chunk Server [%s] listening on topics: %v", chunkID, topics)
	for _, topic := range topics {
		go func(t string) {
			for {
				message, err := kafkaConsumer.ConsumeMessage(t)
				if err != nil {
					log.Printf("Error consuming from topic %s: %v", t, err)
					time.Sleep(2 * time.Second)
					continue
				}
				log.Printf("Received message on [%s]: %s", t, message)
			}
		}(topic)
	}
	select {} // Keep the goroutines running.
}

func isExitTile(row, col int) bool {
	return (row == 0 && col == 8) ||
		(row == 16 && col == 8) ||
		(row == 8 && col == 0) ||
		(row == 8 && col == 16)
}

// canMoveToWithRadius checks if the ghost's circle at (x,y) with the given radius would collide with any wall.
func canMoveToWithRadius(x, y float64, m [][]string, tileSize, radius int) bool {
	minCol := int((x - float64(radius)) / float64(tileSize))
	maxCol := int((x + float64(radius)) / float64(tileSize))
	minRow := int((y - float64(radius)) / float64(tileSize))
	maxRow := int((y + float64(radius)) / float64(tileSize))

	for row := minRow; row <= maxRow; row++ {
		for col := minCol; col <= maxCol; col++ {
			// Out-of-bound cells are considered blocked.
			if row < 0 || row >= len(m) || col < 0 || col >= len(m[0]) {
				return false
			}
			// Treat wall cells and exit tiles as blocked.
			if m[row][col] == "1" || isExitTile(row, col) {
				// Compute the boundaries of the tile.
				rx := float64(col * tileSize)
				ry := float64(row * tileSize)
				// Determine the closest point on the tile to the ghost's center.
				closestX := x
				if x < rx {
					closestX = rx
				} else if x > rx+float64(tileSize) {
					closestX = rx + float64(tileSize)
				}
				closestY := y
				if y < ry {
					closestY = ry
				} else if y > ry+float64(tileSize) {
					closestY = ry + float64(tileSize)
				}
				dx := x - closestX
				dy := y - closestY
				// If the distance is less than the ghost's radius, there's a collision.
				if dx*dx+dy*dy < float64(radius*radius) {
					return false
				}
			}
		}
	}
	return true
}

// chooseNewGhostDirection now uses canMoveToWithRadius so that new directions keep the ghost clear of walls.
func chooseNewGhostDirection(ghost playerstate.GhostState, m [][]string, tileSize int) playerstate.Velocity {
	const ghostRadius = 15
	directions := []playerstate.Velocity{
		{X: 6, Y: 0},  // right
		{X: -6, Y: 0}, // left
		{X: 0, Y: 6},  // down
		{X: 0, Y: -6}, // up
	}

	var mapIndex int
	_, err := fmt.Sscanf(ghost.ID, "map%d_", &mapIndex)
	if err != nil {
		return randomGhostDirection(ghost, m, tileSize, ghostRadius, directions)
	}

	if strings.HasSuffix(ghost.ID, "_ghost_0") {
		return directChaseGhostDirection(ghost, m, tileSize, ghostRadius, directions, mapIndex)
	}

	if strings.HasSuffix(ghost.ID, "_ghost_1") {
		return predictiveChaseGhostDirection(ghost, m, tileSize, ghostRadius, directions, mapIndex)
	}

	return randomGhostDirection(ghost, m, tileSize, ghostRadius, directions)
}

// Existing direct chase logic moved to a separate function
func directChaseGhostDirection(ghost playerstate.GhostState, m [][]string, tileSize, ghostRadius int, directions []playerstate.Velocity, mapIndex int) playerstate.Velocity {
	players := playerstate.GetPlayers()
	var closestPlayer playerstate.PlayerState
	shortestDistance := math.MaxFloat64

	_, err := fmt.Sscanf(ghost.ID, "map%d_", &mapIndex)
	if err != nil {
		return randomGhostDirection(ghost, m, tileSize, ghostRadius, directions)
	}

	// Find closest player on this ghost's map
	for _, player := range players {
		if player.Location.X*4+player.Location.Y == mapIndex {
			distance := math.Hypot(
				ghost.Position.X-player.Position.X,
				ghost.Position.Y-player.Position.Y,
			)

			if distance < shortestDistance {
				shortestDistance = distance
				closestPlayer = player
			}
		}
	}

	if shortestDistance < math.MaxFloat64 {
		var validDirections []playerstate.Velocity

		for _, d := range directions {
			if canMoveToWithRadius(ghost.Position.X+d.X, ghost.Position.Y+d.Y, m, tileSize, ghostRadius) {
				validDirections = append(validDirections, d)
			}
		}

		if len(validDirections) == 0 {
			return playerstate.Velocity{X: 0, Y: 0}
		}

		ghostCellX := int(ghost.Position.X) / tileSize
		ghostCellY := int(ghost.Position.Y) / tileSize

		playerCellX := int(closestPlayer.Position.X) / tileSize
		playerCellY := int(closestPlayer.Position.Y) / tileSize

		horizontalDist := playerCellX - ghostCellX
		verticalDist := playerCellY - ghostCellY

		var preferredDirections []playerstate.Velocity

		if math.Abs(float64(horizontalDist)) > math.Abs(float64(verticalDist)) {
			if horizontalDist > 0 {
				for _, d := range validDirections {
					if d.X > 0 {
						preferredDirections = append(preferredDirections, d)
					}
				}
			} else if horizontalDist < 0 {
				for _, d := range validDirections {
					if d.X < 0 {
						preferredDirections = append(preferredDirections, d)
					}
				}
			}

			if len(preferredDirections) == 0 {
				if verticalDist > 0 {
					for _, d := range validDirections {
						if d.Y > 0 {
							preferredDirections = append(preferredDirections, d)
						}
					}
				} else if verticalDist < 0 {
					for _, d := range validDirections {
						if d.Y < 0 {
							preferredDirections = append(preferredDirections, d)
						}
					}
				}
			}
		} else {
			if verticalDist > 0 {
				for _, d := range validDirections {
					if d.Y > 0 {
						preferredDirections = append(preferredDirections, d)
					}
				}
			} else if verticalDist < 0 {
				for _, d := range validDirections {
					if d.Y < 0 {
						preferredDirections = append(preferredDirections, d)
					}
				}
			}

			if len(preferredDirections) == 0 {
				if horizontalDist > 0 {
					for _, d := range validDirections {
						if d.X > 0 {
							preferredDirections = append(preferredDirections, d)
						}
					}
				} else if horizontalDist < 0 {
					for _, d := range validDirections {
						if d.X < 0 {
							preferredDirections = append(preferredDirections, d)
						}
					}
				}
			}
		}

		if len(preferredDirections) > 0 {
			if ghost.Velocity.X != 0 || ghost.Velocity.Y != 0 {
				reverse := playerstate.Velocity{X: -ghost.Velocity.X, Y: -ghost.Velocity.Y}

				var nonReversingDirections []playerstate.Velocity
				for _, d := range preferredDirections {
					if d.X != reverse.X || d.Y != reverse.Y {
						nonReversingDirections = append(nonReversingDirections, d)
					}
				}

				if len(nonReversingDirections) > 0 {
					idx := time.Now().UnixNano() % int64(len(nonReversingDirections))
					return nonReversingDirections[idx]
				}
			}

			idx := time.Now().UnixNano() % int64(len(preferredDirections))
			return preferredDirections[idx]
		}

		// If no preferred directions, use any valid direction
		idx := time.Now().UnixNano() % int64(len(validDirections))
		return validDirections[idx]
	}
	// If no player found, use random movement
	return randomGhostDirection(ghost, m, tileSize, ghostRadius, directions)
}

// Implement the predictive chase algorithm for ghost_1
func predictiveChaseGhostDirection(ghost playerstate.GhostState, m [][]string, tileSize, ghostRadius int, directions []playerstate.Velocity, mapIndex int) playerstate.Velocity {
	players := playerstate.GetPlayers()
	var targetPlayer playerstate.PlayerState
	playerFound := false

	// Find a player on this ghost's map
	for _, player := range players {
		if player.Location.X*4+player.Location.Y == mapIndex {
			targetPlayer = player
			playerFound = true
			break
		}
	}

	// If no player found, use random movement
	if !playerFound {
		return randomGhostDirection(ghost, m, tileSize, ghostRadius, directions)
	}

	const predictSteps = 8

	predictedX := targetPlayer.Position.X + (targetPlayer.Velocity.X * predictSteps)
	predictedY := targetPlayer.Position.Y + (targetPlayer.Velocity.Y * predictSteps)

	var validDirections []playerstate.Velocity
	for _, d := range directions {
		if canMoveToWithRadius(ghost.Position.X+d.X, ghost.Position.Y+d.Y, m, tileSize, ghostRadius) {
			validDirections = append(validDirections, d)
		}
	}

	if len(validDirections) == 0 {
		return playerstate.Velocity{X: 0, Y: 0}
	}

	if ghost.Velocity.X != 0 || ghost.Velocity.Y != 0 {
		reverse := playerstate.Velocity{X: -ghost.Velocity.X, Y: -ghost.Velocity.Y}

		var nonReversingDirections []playerstate.Velocity
		for _, d := range validDirections {
			if d.X != reverse.X || d.Y != reverse.Y {
				nonReversingDirections = append(nonReversingDirections, d)
			}
		}

		if len(nonReversingDirections) > 0 {
			validDirections = nonReversingDirections
		}
	}

	bestDirection := validDirections[0]
	bestDistance := math.MaxFloat64

	for _, dir := range validDirections {
		nextX := ghost.Position.X + dir.X
		nextY := ghost.Position.Y + dir.Y

		distance := math.Hypot(nextX-predictedX, nextY-predictedY)

		if distance < bestDistance {
			bestDistance = distance
			bestDirection = dir
		}
	}

	return bestDirection
}

// Extract the original random movement logic to a separate function
func randomGhostDirection(ghost playerstate.GhostState, m [][]string, tileSize, ghostRadius int, directions []playerstate.Velocity) playerstate.Velocity {
	allDirections := []playerstate.Velocity{
		{X: 6, Y: 0},  // right
		{X: -6, Y: 0}, // left
		{X: 0, Y: 6},  // down
		{X: 0, Y: -6}, // up
	}

	reverse := playerstate.Velocity{X: -ghost.Velocity.X, Y: -ghost.Velocity.Y}

	var validDirections []playerstate.Velocity
	for _, d := range allDirections {
		if d.X == reverse.X && d.Y == reverse.Y {
			continue
		}
		if canMoveToWithRadius(ghost.Position.X+d.X, ghost.Position.Y+d.Y, m, tileSize, ghostRadius) {
			validDirections = append(validDirections, d)
		}
	}

	if len(validDirections) == 0 {
		if ghost.Velocity.X != 0 || ghost.Velocity.Y != 0 {
			if canMoveToWithRadius(ghost.Position.X+reverse.X, ghost.Position.Y+reverse.Y, m, tileSize, ghostRadius) {
				return reverse
			}
		}
		return playerstate.Velocity{X: 0, Y: 0}
	}

	idx := time.Now().UnixNano() % int64(len(validDirections))
	chosenDirection := validDirections[idx]

	return chosenDirection
}

// updateGhosts continuously updates each ghost's position and velocity.
func updateGhosts() {
	const tileSize = 40
	const ghostRadius = 15
	const centerThreshold = 2
	ticker := time.NewTicker(50 * time.Millisecond)
	defer ticker.Stop()

	directions := []playerstate.Velocity{
		{X: 6, Y: 0},
		{X: -6, Y: 0},
		{X: 0, Y: 6},
		{X: 0, Y: -6},
	}

	for range ticker.C {
		ghosts := playerstate.GetGhosts(chunkKey)
		for id, ghost := range ghosts {
			var mapIndex int
			_, err := fmt.Sscanf(id, "map%d_", &mapIndex)
			if err != nil || mapIndex < 0 || mapIndex >= len(Maps) {
				continue
			}
			m := Maps[mapIndex]

			isChaser := strings.HasSuffix(id, "_ghost_0") || strings.HasSuffix(id, "_ghost_1")

			nextX := ghost.Position.X + ghost.Velocity.X
			nextY := ghost.Position.Y + ghost.Velocity.Y

			if isChaser {
				ghost.Velocity = chooseNewGhostDirection(ghost, m, tileSize)

				nextX := ghost.Position.X + ghost.Velocity.X
				nextY := ghost.Position.Y + ghost.Velocity.Y

				if canMoveToWithRadius(nextX, nextY, m, tileSize, ghostRadius) {
					ghost.Position.X = nextX
					ghost.Position.Y = nextY

					if ghost.Velocity.X != 0 && ghost.Velocity.Y == 0 {
						cellY := math.Floor(ghost.Position.Y / float64(tileSize))
						pathCenterY := (cellY * float64(tileSize)) + (float64(tileSize) / 2)

						adjustment := (pathCenterY - ghost.Position.Y) * 0.2
						if math.Abs(adjustment) > 0.5 {
							ghost.Position.Y += adjustment
						}
					} else if ghost.Velocity.X == 0 && ghost.Velocity.Y != 0 {
						cellX := math.Floor(ghost.Position.X / float64(tileSize))
						pathCenterX := (cellX * float64(tileSize)) + (float64(tileSize) / 2)

						adjustment := (pathCenterX - ghost.Position.X) * 0.2
						if math.Abs(adjustment) > 0.5 {
							ghost.Position.X += adjustment
						}
					}
				} else {
					ghost.Velocity = playerstate.Velocity{X: 0, Y: 0}
				}
			} else {
				if !canMoveToWithRadius(nextX, nextY, m, tileSize, ghostRadius) || (ghost.Velocity.X == 0 && ghost.Velocity.Y == 0) {
					ghost.Velocity = chooseNewGhostDirection(ghost, m, tileSize)

					nextX = ghost.Position.X + ghost.Velocity.X
					nextY = ghost.Position.Y + ghost.Velocity.Y

					if !canMoveToWithRadius(nextX, nextY, m, tileSize, ghostRadius) {
						ghost.Velocity = playerstate.Velocity{X: 0, Y: 0}
					}
				} else {
					ghost.Position.X = nextX
					ghost.Position.Y = nextY

					currentCellX := int(ghost.Position.X) / tileSize
					currentCellY := int(ghost.Position.Y) / tileSize

					tileCenterX := float64(currentCellX*tileSize + tileSize/2)
					tileCenterY := float64(currentCellY*tileSize + tileSize/2)

					distanceToCenter := math.Hypot(
						ghost.Position.X-tileCenterX,
						ghost.Position.Y-tileCenterY,
					)

					if distanceToCenter < centerThreshold {
						if currentCellX >= 0 && currentCellY >= 0 &&
							currentCellX < len(m[0]) && currentCellY < len(m) &&
							m[currentCellY][currentCellX] == "0" {

							ghost.Velocity = randomGhostDirection(ghost, m, tileSize, ghostRadius, directions)

							ghost.Position.X = tileCenterX
							ghost.Position.Y = tileCenterY
						}
					}
				}
			}
			playerstate.UpdateGhostState(chunkKey, ghost)
		}
	}
}
func initializeGhostsForChunk(chunkKey string) {
	for mapIndex := 0; mapIndex < 12; mapIndex++ {
		var ghostPositions []playerstate.Position
		switch mapIndex {
		case 0:
			ghostPositions = []playerstate.Position{
				{X: 260, Y: 300},
				{X: 420, Y: 300},
				{X: 260, Y: 420},
				{X: 420, Y: 420},
			}
		case 1:
			ghostPositions = []playerstate.Position{
				{X: 260, Y: 260},
				{X: 420, Y: 260},
				{X: 260, Y: 420},
				{X: 420, Y: 420},
			}
		case 2:
			ghostPositions = []playerstate.Position{
				{X: 300, Y: 340},
				{X: 380, Y: 340},
				{X: 300, Y: 420},
				{X: 380, Y: 420},
			}
		case 3:
			ghostPositions = []playerstate.Position{
				{X: 260, Y: 220},
				{X: 420, Y: 220},
				{X: 260, Y: 460},
				{X: 420, Y: 460},
			}
		case 4:
			ghostPositions = []playerstate.Position{
				{X: 260, Y: 300},
				{X: 420, Y: 300},
				{X: 260, Y: 380},
				{X: 420, Y: 380},
			}
		case 5:
			ghostPositions = []playerstate.Position{
				{X: 300, Y: 340},
				{X: 340, Y: 300},
				{X: 380, Y: 340},
				{X: 340, Y: 380},
			}
		case 6:
			ghostPositions = []playerstate.Position{
				{X: 260, Y: 260},
				{X: 420, Y: 260},
				{X: 260, Y: 380},
				{X: 420, Y: 380},
			}
		case 7:
			ghostPositions = []playerstate.Position{
				{X: 260, Y: 260},
				{X: 420, Y: 260},
				{X: 260, Y: 420},
				{X: 420, Y: 420},
			}
		case 8:
			ghostPositions = []playerstate.Position{
				{X: 300, Y: 300},
				{X: 380, Y: 300},
				{X: 300, Y: 380},
				{X: 380, Y: 380},
			}
		case 9:
			ghostPositions = []playerstate.Position{
				{X: 260, Y: 220},
				{X: 420, Y: 220},
				{X: 260, Y: 460},
				{X: 420, Y: 460},
			}
		case 10:
			ghostPositions = []playerstate.Position{
				{X: 260, Y: 300},
				{X: 420, Y: 300},
				{X: 260, Y: 380},
				{X: 420, Y: 380},
			}
		case 11:
			ghostPositions = []playerstate.Position{
				{X: 300, Y: 340},
				{X: 340, Y: 300},
				{X: 380, Y: 340},
				{X: 340, Y: 380},
			}
		default:
			ghostPositions = []playerstate.Position{
				{X: 300, Y: 300},
				{X: 360, Y: 300},
				{X: 300, Y: 360},
				{X: 360, Y: 360},
			}
		}
		for i, pos := range ghostPositions {
			ghostID := fmt.Sprintf("map%d_ghost_%d", mapIndex, i)
			ghost := playerstate.GhostState{
				ID:       ghostID,
				Position: pos,
				Velocity: playerstate.Velocity{X: 0, Y: 0},
			}
			playerstate.UpdateGhostState(chunkKey, ghost)
			log.Printf("Initialized ghost %s at position: %+v for map index %d", ghostID, pos, mapIndex)
		}
	}
}

func initializeDroppersForChunk(chunkKey string) {
	for mapIndex := 0; mapIndex < 12; mapIndex++ {

		var initialPos playerstate.Position

		switch mapIndex {
		case 0:
			initialPos = playerstate.Position{X: 260, Y: 220}
		case 1:
			initialPos = playerstate.Position{X: 300, Y: 260}
		case 2:
			initialPos = playerstate.Position{X: 340, Y: 180}
		case 3:
			initialPos = playerstate.Position{X: 260, Y: 300}
		case 4:
			initialPos = playerstate.Position{X: 300, Y: 300}
		case 5:
			initialPos = playerstate.Position{X: 280, Y: 280}
		case 6:
			initialPos = playerstate.Position{X: 300, Y: 260}
		case 7:
			initialPos = playerstate.Position{X: 260, Y: 300}
		case 8:
			initialPos = playerstate.Position{X: 340, Y: 300}
		case 9:
			initialPos = playerstate.Position{X: 260, Y: 300}
		case 10:
			initialPos = playerstate.Position{X: 300, Y: 260}
		case 11:
			initialPos = playerstate.Position{X: 320, Y: 300}
		default:
			initialPos = playerstate.Position{X: 260, Y: 260}
		}

		dropperID := fmt.Sprintf("map%d_dropper_1", mapIndex)
		ds := playerstate.DropperState{
			ID:            dropperID,
			Position:      initialPos,
			LastPosition:  initialPos,
			Velocity:      playerstate.Velocity{X: 0, Y: 0},
			PelletCounter: 0,
		}
		playerstate.UpdateDropperState(ds)
		log.Printf("Initialized dropper %s at position: %+v", dropperID, ds.Position)
	}
}

func chooseNewDropperDirection(ds playerstate.DropperState, m [][]string, tileSize int) playerstate.Velocity {
	const dropperRadius = 15
	directions := []playerstate.Velocity{
		{X: 6, Y: 0},
		{X: -6, Y: 0},
		{X: 0, Y: 6},
		{X: 0, Y: -6},
	}
	var candidates []playerstate.Velocity
	reverse := playerstate.Velocity{X: -ds.Velocity.X, Y: -ds.Velocity.Y}
	for _, d := range directions {
		if d.X == reverse.X && d.Y == reverse.Y {
			continue
		}
		if canMoveToWithRadius(ds.Position.X+d.X, ds.Position.Y+d.Y, m, tileSize, dropperRadius) {
			candidates = append(candidates, d)
		}
	}
	if len(candidates) == 0 {
		return playerstate.Velocity{X: 0, Y: 0}
	}
	idx := time.Now().UnixNano() % int64(len(candidates))
	return candidates[idx]
}

func updateDroppers() {
	const tileSize = 40
	const dropperRadius = 15
	const pelletPlacementInterval = 5
	const centerThreshold = 5

	ticker := time.NewTicker(50 * time.Millisecond)
	defer ticker.Stop()

	for range ticker.C {
		droppers := playerstate.GetDroppers()

		for _, ds := range droppers {

			var mapIndex int
			_, err := fmt.Sscanf(ds.ID, "map%d_", &mapIndex)
			if err != nil {
				continue
			}
			if mapIndex < 0 || mapIndex >= len(Maps) {
				continue
			}

			m := Maps[mapIndex]
			ds.LastPosition = ds.Position
			nextX := ds.Position.X + ds.Velocity.X
			nextY := ds.Position.Y + ds.Velocity.Y

			if !canMoveToWithRadius(nextX, nextY, m, tileSize, dropperRadius) || (ds.Velocity.X == 0 && ds.Velocity.Y == 0) {
				ds.Velocity = chooseNewDropperDirection(ds, m, tileSize)
			} else {
				ds.Position.X = nextX
				ds.Position.Y = nextY

				currentCellX := int(ds.Position.X) / tileSize
				currentCellY := int(ds.Position.Y) / tileSize

				tileCenterX := float64(currentCellX*tileSize + tileSize/2)
				tileCenterY := float64(currentCellY*tileSize + tileSize/2)

				distanceToCenter := math.Hypot(
					ds.Position.X-tileCenterX,
					ds.Position.Y-tileCenterY,
				)

				if distanceToCenter < centerThreshold {
					if currentCellX >= 0 && currentCellY >= 0 &&
						currentCellX < len(m[0]) && currentCellY < len(m) &&
						m[currentCellY][currentCellX] == "0" {

						ds.Velocity = chooseNewDropperDirection(ds, m, tileSize)

						ds.Position.X = tileCenterX
						ds.Position.Y = tileCenterY
					}
				}
			}

			ds.PelletCounter++

			if ds.PelletCounter >= pelletPlacementInterval {
				ds.PelletCounter = 0

				cellX := int(ds.LastPosition.X) / tileSize
				cellY := int(ds.LastPosition.Y) / tileSize

				cellCenterX := float64(cellX*tileSize + tileSize/2)
				cellCenterY := float64(cellY*tileSize + tileSize/2)

				cellCenterXInt := int(cellCenterX)
				cellCenterYInt := int(cellCenterY)

				if cellX >= 0 && cellY >= 0 && cellX < len(m[0]) && cellY < len(m) && m[cellY][cellX] == "0" {
					cellPelletID := fmt.Sprintf("%d-%d", cellCenterXInt, cellCenterYInt)

					if playerstate.IsPelletEaten(cellPelletID, mapIndex) {
						pelletID := fmt.Sprintf("pellet-%d-%d-%d", cellX, cellY, mapIndex)
						restoredPellet := playerstate.RestoredPellet{
							ID: pelletID,
							Position: playerstate.Position{
								X: cellCenterX,
								Y: cellCenterY,
							},
							MapIndex: mapIndex,
						}
						playerstate.AddRestoredPellet(chunkKey, restoredPellet)
						playerstate.UnmarkPellet(cellPelletID, mapIndex)
					}
				}
			}

			playerstate.UpdateDropperState(ds)
		}
	}
}

func main() {
	// Set kafkaBroker from environment variable.
	kafkaBroker = os.Getenv("KAFKA_BOOTSTRAP_SERVER")
	if kafkaBroker == "" {
		kafkaBroker = "kafka:9092"
	}

	// Generate the chunk server's unique ID.
	chunkID = generateChunkID()

	// Read the raw coordinate from environment variable and remove any double quotes.
	rawCoord := os.Getenv("CHUNK_COORDINATE")
	if rawCoord == "" {
		log.Println("CHUNK_COORDINATE not found in env; using default coordinate '0,0'")
		rawCoord = "0,0"
	} else {
		rawCoord = strings.ReplaceAll(rawCoord, "\"", "")
	}
	// Use the cleaned raw coordinate for map requests.
	chunkCoordinate = rawCoord
	// For the Kafka topic name, replace commas with underscores.
	chunkTopic = fmt.Sprintf("central_to_chunk_%s", strings.ReplaceAll(rawCoord, ",", "_"))
	chunkKey = strings.ReplaceAll(rawCoord, ",", "-")
	log.Printf("Chunk ID: %s", chunkID)
	log.Printf("Chunk topic: %s", chunkTopic)

	playerstate.CurrentChunkKey = chunkKey

	// Initialize Kafka producer.
	producer, err := kafka.NewProducer(kafkaBroker)
	if err != nil {
		log.Fatalf("Failed to initialize Kafka producer: %v", err)
	}
	kafkaProducer = producer

	// Create the Kafka topic for this chunk server.
	err = kafkaProducer.CreateTopic(chunkTopic)
	if err != nil {
		log.Fatalf("Failed to create Kafka topic: %v", err)
	}
	log.Printf("Kafka topic '%s' created", chunkTopic)

	// Initialize main Kafka consumer.
	consumer, err := kafka.NewConsumer(kafkaBroker, chunkID)
	if err != nil {
		log.Fatalf("Failed to initialize Kafka consumer: %v", err)
	}
	kafkaConsumer = consumer

	// Notify central server that this chunk server is connected.
	msg := fmt.Sprintf("Chunk server with coordinate [%s] and ID [%s] is connected", chunkCoordinate, chunkID)
	err = kafkaProducer.SendMessage(centralTopic, msg)
	if err != nil {
		log.Printf("Failed to notify central server via Kafka: %v", err)
	} else {
		log.Printf("Notified central server: %s", msg)
	}

	// Request the map from central server using the dedicated consumer.
	go requestMap()

	// Start consuming Kafka messages on the main consumer.
	go consumeMessages()

	// Launch ghost initialization and updater after the map is loaded.
	initializeGhostsForChunk(chunkKey)

	initializeDroppersForChunk(chunkKey)

	// Launch ghost updaters for all maps concurrently.
	go updateGhosts()

	go updateDroppers()

	// Start a ticker to broadcast game state to connected WebSocket clients.
	go func() {
		ticker := time.NewTicker(50 * time.Millisecond)
		defer ticker.Stop()
		for range ticker.C {
			websocket.BroadcastGameState()
		}
	}()

	// Instead of sending aggregated player state on a fixed frequency,
	// we track the last sent state and only send an update when there is a change.
	type PlayerUpdate struct {
		UserName string `json:"userName"`
		Score    int    `json:"score"`
		Status   string `json:"status"`
	}
	lastSentStates := make(map[string]PlayerUpdate)

	go func() {
		// We can still check periodically (every second) for any state changes.
		ticker := time.NewTicker(1 * time.Second)
		defer ticker.Stop()
		for range ticker.C {
			players := playerstate.GetGameState()
			for _, ps := range players {
				// Create the update for this player.
				update := PlayerUpdate{
					UserName: ps.ID,
					Score:    ps.Score,
					Status:   ps.Status,
				}
				// Check if we have a previously sent state.
				last, ok := lastSentStates[ps.ID]
				// If no previous state exists or if any field has changed, send an update.
				if !ok || last.Score != update.Score || last.Status != update.Status {
					msgData, err := json.Marshal(update)
					if err != nil {
						log.Printf("Error marshaling player update: %v", err)
						continue
					}
					err = kafkaProducer.SendMessage(centralTopic, string(msgData))
					if err != nil {
						log.Printf("Error sending player update to central: %v", err)
						continue
					}
					// Update the cache.
					lastSentStates[ps.ID] = update
					log.Printf("Sent update for player %s: %v", ps.ID, update)
				}
			}
		}
	}()

	// Graceful shutdown handling.
	go func() {
		sigChan := make(chan os.Signal, 1)
		signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)
		<-sigChan
		log.Println("Shutting down Chunk Server...")
		kafkaProducer.Close()
		kafkaConsumer.Close()
		os.Exit(0)
	}()

	r := setupRouter()
	r.Run(":8080")
}
