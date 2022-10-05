package main

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"net"
	"sync"
	"time"

	VEIv1_0 "VEIv1.0/api"
	"google.golang.org/grpc"
	Emptypb "google.golang.org/protobuf/types/known/emptypb"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/iotdataplane"
	mqtt "github.com/eclipse/paho.mqtt.golang"
	"github.com/golang-jwt/jwt"
	"github.com/nats-io/nats.go"
)

type server struct {
	VEIv1_0.UnimplementedVEIv1_0Server
}

const (
	port = ":50051"
)

var availCameras = make([]string, 0)
var availApplications = make([]string, 0)
var availClouds = make([]string, 0)
var iotCore *iotdataplane.IoTDataPlane
var mqttCli mqtt.Client
var topic string

func checkCamera(cameraID string) bool {
	for _, val := range availCameras {
		if val == cameraID {
			return true
		}
	}

	return false
}

func checkApplications(applicationName string) bool {
	for _, val := range availApplications {
		if val == applicationName {
			return true
		}
	}
	return false
}

func checkClouds(cloudName string) bool {
	for _, val := range availClouds {
		if val == cloudName {
			return true
		}
	}
	return false
}

// AWS IoT
func connectToAWSIoT() *iotdataplane.IoTDataPlane {
	//AWS Parameters
	sess, err := session.NewSession(&aws.Config{
		Region:   aws.String("us-east-1"),
		Endpoint: aws.String("a2vfzs8j0eo870-ats.iot.us-east-1.amazonaws.com")},
	)
	if err != nil {
		log.Fatal("failed to create session")
	}

	availClouds = append(availClouds, "AWS")

	return iotdataplane.New(sess)

}

// GCP IoT
func connectToGCPIoT() (mqtt.Client, string) {
	const (
		mqttBrokerURL   = "tls://mqtt.googleapis.com:8883"
		protocolVersion = 4
	)

	//Configurations for tls -- using Google's root CA cert
	certpool := x509.NewCertPool()
	pemCerts, err := ioutil.ReadFile("./gcp/roots.pem")
	if err == nil {
		certpool.AppendCertsFromPEM(pemCerts)
	} else {
		log.Fatal(err)
	}

	config := &tls.Config{
		RootCAs:            certpool,
		ClientAuth:         tls.NoClientCert,
		ClientCAs:          nil,
		InsecureSkipVerify: true,
		Certificates:       []tls.Certificate{},
		MinVersion:         tls.VersionTLS12,
	}

	// As set up on IoT Core console
	projectID := "visionedgeiot"
	registryID := "edge-registry"
	region := "us-central1"   // Do not change
	deviceID := "soundgarden" // Name of Edge server

	//Create clientID
	clientID := fmt.Sprintf("projects/%s/locations/%s/registries/%s/devices/%s", projectID, region, registryID, deviceID)

	// onConnect defines the on connect handler which resets backoff variables.
	var onConnect mqtt.OnConnectHandler = func(client mqtt.Client) {
		log.Printf("Client connected: %t\n", client.IsConnected())
	}

	// onMessage defines the message handler for the mqtt client.
	var onMessage mqtt.MessageHandler = func(client mqtt.Client, msg mqtt.Message) {
		log.Printf("Topic: %s\n", msg.Topic())
		log.Printf("Message: %s\n", msg.Payload())
	}

	// onDisconnect defines the connection lost handler for the mqtt client.
	var onDisconnect mqtt.ConnectionLostHandler = func(client mqtt.Client, err error) {
		log.Println("Client disconnected")
	}

	// Create new JSON Web Token using RS256
	jwtToken := jwt.New(jwt.SigningMethodRS256)
	jwtToken.Claims = jwt.StandardClaims{
		Audience:  projectID,
		IssuedAt:  time.Now().Unix(),
		ExpiresAt: time.Now().Add(24 * time.Hour).Unix(),
	}

	keyBytes, err := ioutil.ReadFile("./gcp/rsa_private.pem")
	if err != nil {
		log.Fatal(err)
	}

	privKey, err := jwt.ParseRSAPrivateKeyFromPEM(keyBytes)
	if err != nil {
		log.Fatal(err)
	}

	jwtTLSPass, err := jwtToken.SignedString(privKey)
	if err != nil {
		log.Fatal(err)
	}

	//options for creating a new mqtt client
	opts := mqtt.NewClientOptions()
	opts.AddBroker(mqttBrokerURL)
	opts.SetClientID(clientID).SetTLSConfig(config)
	opts.SetUsername("unused")
	opts.SetPassword(jwtTLSPass)
	opts.SetProtocolVersion(protocolVersion)
	opts.SetOnConnectHandler(onConnect)
	opts.SetDefaultPublishHandler(onMessage)
	opts.SetConnectionLostHandler(onDisconnect)

	//Create new mqtt client
	mqttCli := mqtt.NewClient(opts)

	//connecting
	if connectedToken := mqttCli.Connect(); connectedToken.Wait() && connectedToken.Error() != nil {
		log.Println("Failed to connect client.")
	}

	//Set topic to be /devices/deviceID/events --> Will publish to topic based on default topic in the device on GCP
	topic := fmt.Sprintf("/devices/%v/events", deviceID)

	return mqttCli, topic

}

func (s *server) PublishImage(stream VEIv1_0.VEIv1_0_PublishImageServer) error {

	//error message
	errMsg := &VEIv1_0.ErrorResponse{}

	//start a new connection with NATS
	var nc, err = nats.Connect(nats.DefaultURL)
	if err != nil {
		panic(err)
	}
	defer nc.Close()

	natsC, err := nats.NewEncodedConn(nc, nats.JSON_ENCODER)
	if err != nil {
		panic(err)
	}
	defer natsC.Close()

	//JSON object for NATS
	type imageData struct {
		Image     []byte
		Timestamp string
	}

	//streaming loop
	for {
		//start receiving messages to stream from Camera
		req, err := stream.Recv()

		//check if the stream has finished
		if err == io.EOF {
			//close NATS and the streaming loop
			nc.Close()
			natsC.Close()
			return stream.SendAndClose(errMsg)
		}

		//individual data from request
		cameraID := req.GetCameraID()
		image := req.GetImage()
		timestamp := req.GetTimestamp()

		if !checkCamera(cameraID) {
			availCameras = append(availCameras, cameraID)
		}

		//Publish data
		if err := natsC.Publish(cameraID, &imageData{Image: image, Timestamp: timestamp}); err != nil {
			errMsg.ErrorMsg = "Error publishing image"
		}
	}
}

func (s *server) SubscribeImage(in *VEIv1_0.SubImageParams, stream VEIv1_0.VEIv1_0_SubscribeImageServer) error {
	cameraID := in.GetCameraID()

	//start a new connection with NATS
	nc, err := nats.Connect(nats.DefaultURL,
		nats.ErrorHandler(func(nc *nats.Conn, s *nats.Subscription, err error) {
			if s != nil {
				log.Printf("Async error in %q/%q: %v", s.Subject, s.Queue, err)
			} else {
				log.Printf("Async error outside subscription: %v", err)
			}
		}))
	if err != nil {
		panic(err)
	}
	defer nc.Close()

	natsC, err := nats.NewEncodedConn(nc, nats.JSON_ENCODER)
	if err != nil {
		log.Fatal(err)
		panic(err)
	}
	defer natsC.Close()

	//JSON object for NATS
	type imageData struct {
		Image     []byte
		Timestamp string
	}

	wg := sync.WaitGroup{}
	wg.Add(1)

	//subscribe and decode
	if _, err := natsC.Subscribe(cameraID, func(newImage *imageData) {
		natsMsg := VEIv1_0.ImageData{Image: newImage.Image, Timestamp: newImage.Timestamp, Error: ""}

		if err := stream.Send(&natsMsg); err != nil {
			log.Fatal(err)
		}
	}); err != nil {
		log.Fatal(err)
	}

	//Wait for messages to arrive
	wg.Wait()
	//close the connection
	nc.Close()
	natsC.Close()

	return nil
}

func (s *server) PublishVisionOutput(stream VEIv1_0.VEIv1_0_PublishVisionOutputServer) error {

	errMsg := &VEIv1_0.ErrorResponse{}

	var nc, err = nats.Connect(nats.DefaultURL)
	if err != nil {
		panic(err)
	}
	defer nc.Close()

	natsC, err := nats.NewEncodedConn(nc, nats.JSON_ENCODER)
	if err != nil {
		panic(err)
	}
	defer natsC.Close()

	type visionOutputData struct {
		VisionOutput []string
		Timestamp    string
	}

	for {
		req, err := stream.Recv()

		if err == io.EOF {
			nc.Close()
			natsC.Close()
			return stream.SendAndClose(errMsg)
		}

		visionAppName := req.GetVisionAppName()
		visionOutput := req.GetVisionOutput()
		timestamp := req.GetTimestamp()

		if !checkApplications(visionAppName) {
			availApplications = append(availApplications, visionAppName)
		}

		if err := natsC.Publish(visionAppName, &visionOutputData{VisionOutput: visionOutput, Timestamp: timestamp}); err != nil {
			errMsg.ErrorMsg = "Error publishing vision output"
		}
	}
}

func (s *server) SubscribeVisionOutput(in *VEIv1_0.SubVisionParams, stream VEIv1_0.VEIv1_0_SubscribeVisionOutputServer) error {
	visionAppName := in.GetVisionAppName()

	nc, err := nats.Connect(nats.DefaultURL,
		nats.ErrorHandler(func(nc *nats.Conn, s *nats.Subscription, err error) {
			if s != nil {
				log.Printf("Async error in %q/%q: %v", s.Subject, s.Queue, err)
			} else {
				log.Printf("Async error outside subscription: %v", err)
			}
		}))
	if err != nil {
		panic(err)
	}
	defer nc.Close()

	natsC, err := nats.NewEncodedConn(nc, nats.JSON_ENCODER)
	if err != nil {
		panic(err)
	}
	defer natsC.Close()

	type visionOutputData struct {
		VisionOutput []string
		Timestamp    string
	}

	wg := sync.WaitGroup{}
	wg.Add(1)

	if _, err := natsC.Subscribe(visionAppName, func(newVisionData *visionOutputData) {
		natsMsg := VEIv1_0.VisionOutput{VisionOutput: newVisionData.VisionOutput, Timestamp: newVisionData.Timestamp, Error: ""}
		if err := stream.Send(&natsMsg); err != nil {
			log.Fatal(err)
		}
	}); err != nil {
		log.Fatal(err)
		return err
	}

	wg.Wait()
	nc.Close()
	natsC.Close()

	return nil
}

func (s *server) PublishToCloud(stream VEIv1_0.VEIv1_0_PublishToCloudServer) error {

	errMsg := &VEIv1_0.ErrorResponse{}

	for {
		req, err := stream.Recv()

		if err == io.EOF {
			return stream.SendAndClose(errMsg)
		}

		analyticsName := req.GetAnalyticsName()
		analyticalOutput := req.GetAnalyticalOutput()
		timestamp := req.GetTimestamp()
		cloudProvider := req.GetCloudProvider()

		log.Println(cloudProvider)

		if !checkApplications(analyticsName) {
			availApplications = append(availApplications, analyticsName)
		}

		type output struct {
			AnalyticalOutput []string
			Timestamp        string
		}

		var jsonOutput = &output{AnalyticalOutput: analyticalOutput, Timestamp: timestamp}
		var completeOutput, _ = json.Marshal(jsonOutput)

		if cloudProvider == "AWS" {
			publishingParams := &iotdataplane.PublishInput{
				Topic:   aws.String(analyticsName),
				Payload: completeOutput,
				Qos:     aws.Int64(1),
			}

			iotCore.Publish(publishingParams)

		} else if cloudProvider == "GCP" {
			//Publish with QoS = 0, Retained = false, payload
			publishedToken := mqttCli.Publish(topic, 0, false, completeOutput)
			publishedToken.Wait()
			if publishedToken.Error() != nil {
				log.Fatal(publishedToken.Error())
			}

		} else {
			log.Fatal("Cloud provider specified not supported")

		}

	}
}

func (s *server) DeleteCamera(ctx context.Context, in *VEIv1_0.CameraID) (*VEIv1_0.ErrorResponse, error) {
	cameraID := in.GetCameraID()
	errMsg := &VEIv1_0.ErrorResponse{}
	if checkCamera(cameraID) {
		for indx, val := range availCameras {
			if val == cameraID {
				availCameras = append(availCameras[:indx], availCameras[indx+1:]...)
				errMsg.ErrorMsg = ""
				return errMsg, nil
			}
		}
		errMsg.ErrorMsg = ""
		return errMsg, nil
	} else {
		errMsg.ErrorMsg = "Camera does not exist"
		return errMsg, errors.New("CAMERA DNE")
	}
}

func (s *server) ListCameras(context.Context, *Emptypb.Empty) (*VEIv1_0.Cameras, error) {
	cameras := &VEIv1_0.Cameras{}
	cameras.AvailableCams = availCameras
	log.Println(cameras)

	return cameras, nil
}

func (s *server) ListApplications(context.Context, *Emptypb.Empty) (*VEIv1_0.Applications, error) {
	applications := &VEIv1_0.Applications{}
	applications.AvailApps = availApplications
	log.Println(applications)

	return applications, nil
}

func (s *server) ListClouds(context.Context, *Emptypb.Empty) (*VEIv1_0.Clouds, error) {
	clouds := &VEIv1_0.Clouds{}
	clouds.AvailClouds = availClouds
	log.Println(clouds)

	return clouds, nil
}

func main() {
	//Start listening to tcp port, if cannot connect then throw an error
	listen, err := net.Listen("tcp", port)
	if err != nil {
		log.Fatalf("failed to listen: %v", err)
	}

	//start the new server with grpc
	s := grpc.NewServer()
	VEIv1_0.RegisterVEIv1_0Server(s, &server{})

	// Connnect to cloud service procviders
	iotCore = connectToAWSIoT()
	mqttCli, topic = connectToGCPIoT()

	log.Println("Connected to AWS and GCP")

	if err := s.Serve(listen); err != nil {
		log.Fatalf("failed to serve: %v", err)
	}
}
