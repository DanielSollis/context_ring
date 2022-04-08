package main

import (
	pb "Context_Ring/context_ring"
	"context"
	"flag"
	"fmt"
	"log"
	"math/rand"
	"net"
	"os"
	"os/signal"
	"time"

	"google.golang.org/grpc"
)

var (
	serverOrClient = flag.String("s", "server", "Is the program a server or client")
	port           = flag.Int64("p", 9000, "The Server's Port number")
	name           = flag.String("n", "DefaultName", "The Server's name")
	terminalNode   = flag.Bool("t", false, "If the node is the last on the ring")
	firstHop       = flag.Int64("f", 9001, "The client's first port to visit")
	timeOut        = flag.Int64("d", 10, "Timeout time in seconds")
)

type server struct {
	pb.UnimplementedWorkerServer
}

func main() {
	flag.Parse()
	if *serverOrClient == "server" {
		serving()
	} else if *serverOrClient == "client" {
		clienting()
	} else {
		log.Fatalf("Program must be a \"server\" or a \"client\", not a \"%s\"", *serverOrClient)
	}
}

// Instantiate a gRPC worker server and listener and start listening on
// the specified port
func serving() {
	listener, err := net.Listen("tcp", fmt.Sprintf("localhost:%d", *port))
	defer listener.Close()
	if err != nil {
		log.Fatalf("failed to listen on localhost:%d, %v", *port, err)
	}
	workServer := grpc.NewServer()
	pb.RegisterWorkerServer(workServer, &server{})

	// Handle ctrl+C interrupt signals
	c := make(chan os.Signal, 2)
	signal.Notify(c, os.Interrupt)
	go func() {
		<-c
		fmt.Printf("\nStopping %s server on localhost:%d\n", *name, *port)
		workServer.Stop()
		os.Exit(1)
	}()

	log.Printf("server %s listening at localhost:%d\n", *name, *port)
	if err := workServer.Serve(listener); err != nil {
		log.Fatalf("server %s failed to serve: %s", *name, err)
	}
}

// Opens a connection to the specified client port, instantiates a new gRPC
// worker client, creates the first message and context to be passed to the
// initial server, passes them to that server, and prints the final trace after
// it has passed through each server in the ring
func clienting() {
	fmt.Printf("Connecting to port %d\n", *firstHop)
	conn, err := grpc.Dial(fmt.Sprintf("localhost:%d", *firstHop), grpc.WithInsecure())
	if err != nil {
		log.Fatalf("could not connect to localhost:%d\n", *firstHop)
	}
	defer conn.Close()
	client := pb.NewWorkerClient(conn)

	messageToPass := &pb.WorkMessage{CurrentPort: *firstHop, Trace: *name}
	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(*timeOut)*time.Second)
	defer cancel()
	fmt.Printf("Passing to %d with message \"%v\"\n", *firstHop, messageToPass)
	response, err := client.PassToNext(ctx, messageToPass)
	if err != nil {
		log.Fatalf("Server call error: %s\n", err)
	}

	lastTrace := fmt.Sprintf("%s -> %s", response.GetTrace(), *name)
	fmt.Printf("round trip: %s\n", lastTrace)
	os.Exit(0)
}

// gRPC request handler that does some work (sleeps), calculates the next server
// port (increments by one), adds to the passed trace, and passes the context,
// next server port and the modified trace to dialNext(), awaiting a response
// to return
func (s *server) PassToNext(ctx context.Context, in *pb.WorkMessage) (*pb.WorkMessage, error) {
	workDone := doWork()
	fmt.Printf("work done: %f\n", workDone)
	var nextHop int64
	newTrace := fmt.Sprintf("%s -> %s", in.GetTrace(), *name)
	defer log.Printf("server %s listening at localhost:%d\n", *name, *port)
	if !*terminalNode {
		nextHop = in.GetCurrentPort() + 1
	} else {
		fmt.Printf("going home!\n")
		in.Trace = newTrace
		return in, nil
	}
	response, err := dialNext(ctx, nextHop, newTrace)
	if err != nil {
		return nil, err
	}
	return response, nil
}

// Calculates a random float64 between 0 and 4, sleeps for that long,
// and then returns that float
func doWork() float64 {
	rand.Seed(time.Now().Unix())
	workToDo := rand.Float64() * 4
	time.Sleep(time.Duration(workToDo * float64(time.Second)))
	return workToDo
}

// Dials the next server in the ring, creates a new WorkMessage with the passed
// port and trace, then passes the message to the next server and awaits a
// response to return
func dialNext(ctx context.Context, nextHop int64, newTrace string) (*pb.WorkMessage, error) {
	fmt.Printf("dialing next hop: %d\n", nextHop)
	conn, err := grpc.Dial(fmt.Sprintf("localhost:%d", nextHop), grpc.WithInsecure())
	if err != nil {
		log.Fatalf("could not connect to %d: %s", nextHop, err)
	}
	defer conn.Close()
	server := pb.NewWorkerClient(conn)
	messageToPass := &pb.WorkMessage{CurrentPort: nextHop, Trace: newTrace}
	fmt.Printf("executing hop to port %d with message \"%v\"\n", nextHop, messageToPass)
	response, err := server.PassToNext(ctx, messageToPass)
	if err != nil {
		log.Fatalf("received error from %d: %s", nextHop, err)
	}
	return response, err
}
