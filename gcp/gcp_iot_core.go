// Sample publisher to Google IoT Core
// Set up as described here - https://cloud.google.com/iot/docs/create-device-registry
// Subscribe first from the console

package main

import (
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"io/ioutil"
	"log"
	"time"

	mqtt "github.com/eclipse/paho.mqtt.golang"
	jwt "github.com/golang-jwt/jwt"
)

//Using open source paho mqtt

//Devices are already bounded to gateways via the GCP
//Starts a gateway client that sends data on behalf of a bound device

func main() {

	const (
		mqttBrokerURL   = "tls://mqtt.googleapis.com:8883"
		protocolVersion = 4
	)

	//Configurations for tls -- using Google's root CA cert
	certpool := x509.NewCertPool()
	pemCerts, err := ioutil.ReadFile("./roots.pem")
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
	region := "us-central1"
	deviceID := "soundgarden"

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

	keyBytes, err := ioutil.ReadFile("./rsa_private.pem")
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

	//Optional delay
	time.Sleep(1 * time.Second)

	//Publish with QoS = 0, Retained = false, payload
	publishedToken := mqttCli.Publish(topic, 0, false, []byte("test"))
	publishedToken.Wait()
	if publishedToken.Error() != nil {
		log.Fatal(publishedToken.Error())
	}

	// close the client
	mqttCli.Disconnect(250)

}
