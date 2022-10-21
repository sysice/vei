VEI edge gateway (Golang) and clients (Python) as described in our Middlewedge 2022 paper, ""VEI: A Multicloud Edge Gateway for Computer Vision in IoT"  
Authors: Samantha Luu, Arun Ravindran, Armin Danesh Pazho, and Hamed Tabkhi  
Department of Electrical and Computer Engineering, UNC Charlotte

Installation instructions

Dependencies: Docker, Golang, Python3, gRPC (For Go and Python, https://grpc.io/docs/languages/go/quickstart/)
Other dependencies: As specified in the imports
$ pip3 install opencv-python


api/VEIv1\_0.proto: VEI APIs
server.go: VEI RPC server
camera.py: RPC client that captures webcam image and publishes to VEI
yolo.py: RPC client that ubscribes images from VEI, calls YOLO service for object recogntion, and publishes objects to Cloud (AWS IoT Core, GCP IoT Core)

Run NATS streaming as a Docker container at standard NATS port 
$ sudo docker run -d --rm  --name nats -p 4222:4222 nats

Run application as a container at port 8080. As an example, we use the YOLOv3 container published by Johannes Tang Kristensen
$ sudo docker run -d --rm --name yolo\_service -p 8080:8080 johannestang/yolo\_service:1.0-yolov3\_coco

Check if containers are running
$ sudo docker container ls

Create an images directory. YOLOv3 reads video frames from this directory
$ mkdir images

Change cloud parameters according to your setup in server.go

For AWS - 
Use AWS CLI to create AWS IoT Core Thing
https://gist.github.com/noahcoad/d2ac692b487200559a6a0a0b8762a690

Store credentials and configuration
~/.aws/credentials 
[default]
aws\_access\_key\_id=<your key from account>
aws\_secret\_access\_key=<your secret access key from account>

~/.aws/config
[default]
region=<your aws region>
output=json


Insert the endpoint obtained above in connectToAWSIoT() in server.go
On the AWS IoT Core console, under Test, MQTT Test client, specify the topic (for example, camera1 from yolo.py) to subscribe, and click Subscribe to topic

To run, from 3 terminals on the edge server-
$ go run server.go
$ python3 yolo.py
$ python3 camera.py

The object recognition output from the individual camera frames should appear on the AWS IoT Core subscription 


GCP IoT Core follows a conceptually similar setup. Google will be retiring this service in Aug 2023.

