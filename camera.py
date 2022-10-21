from ctypes import sizeof
import cv2
from time import sleep
import grpc
import sys
import os
import time
import csv

from numpy import byte, full

API_PATH = './api'

sys.path.insert(0, API_PATH)
import VEIv1_0_pb2_grpc as VEI_grpc
import VEIv1_0_pb2 as VEI

def publish_request(numFrames):
    cam = cv2.VideoCapture(0) 
    fdIm = open("imcapture_timestamps.txt", "w")
    for i in range(numFrames):                           

        sleep(1.5)  # Wait 1.5 second
        ret_val, frame = cam.read()                     # start reading from camera
        #cv2.imshow('webcam', frame)                   # uncomment to see webcam view
        #cv2.waitKey(0)
       
        # Timestamp at which video frame captured
        im_timestamp = (str)(time.time())
        fdIm.write(im_timestamp+'\n')

        im_success, im_arr = cv2.imencode(".jpg", frame)  
        byte_im = im_arr.tobytes()
        
        #create request for publishing
        req = VEI.PubImageParams(cameraID = "camera1", image = byte_im, timestamp = im_timestamp)

        # Listing Cameras:
        # camListReq = VEI.google_dot_protobuf_dot_empty__pb2.Empty()
        # stub.ListCameras(camListReq)

        yield req
        #yield statement suspends function’s execution and sends a value back to the caller, but retains enough state to enable function to resume where it is left off. 
        
    cam.release()
    cv2.destroyAllWindows()
    fdIm.close()


if __name__ == '__main__':
    host = 'localhost'
    server_port = 50051

    # instantiate a channel
    channel = grpc.insecure_channel(
        '{}:{}'.format(host, server_port)
    )

    # bind the client and the server
    stub = VEI_grpc.VEIv1_0Stub(channel)
   
    stub.PublishImage(publish_request(2)) # Specify the number of frames to be captured

  
    
