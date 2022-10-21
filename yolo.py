import os
import numpy as np
import requests
import cv2
import json
import sys
from datetime import datetime
import grpc
import time

from time import sleep

API_PATH = './api'
IMAGES_FOLDER = "./images"

sys.path.insert(0, API_PATH)
import VEIv1_0_pb2_grpc as VEI_grpc
import VEIv1_0_pb2 as VEI

fdCV = open("cv_timestamps.txt", "w")


def YOLO(im, frameNum, imageTimestamp, cloud):
    imagesFolder = IMAGES_FOLDER
    im_path = os.path.join(imagesFolder, 'Frame'+ str(frameNum) + '.jpg')
    cv2.imwrite(im_path, im)
    print(cloud)
    image = {'image_file': open(im_path, 'rb')}
    data = {'threshold': '0.25'}
    yolo_url = 'http://localhost:8080/'
    url = yolo_url + "detect"
    r = requests.post(url, files=image, data=data)
    if r.status_code == 200:
        details = {"object": None, "prob": None}
        for item in r.json():
            details['object'] = item[0]
            details['prob'] = item[1]
            s = json.dumps(details)
            print(s)
            req = VEI.PubCloudParams(analyticsName = "camera1", analyticalOutput = s, timestamp = imageTimestamp, cloudProvider = cloud)
            yield req
    

def main():
    frameNum = 0
    host = 'localhost'
    server_port = 50051

    #instantiate a channel
    channel = grpc.insecure_channel(
            '{}:{}'.format(host, server_port)
    )

    # bind the client and the server
    stub = VEI_grpc.VEIv1_0Stub(channel)

    #make a new request to specific subject
    im_data_req = VEI.SubImageParams(cameraID="camera1")
    
    for msg in stub.SubscribeImage(im_data_req):
        unBytes_im = np.frombuffer(msg.image, dtype=np.uint8)
        data_im = cv2.imdecode(unBytes_im, cv2.IMREAD_UNCHANGED)
        frameNum += 1
        # Timestamping CV
        before_timestamp = (str)(time.time())
        pubYoloreq = YOLO(data_im, frameNum, msg.timestamp, "AWS")
        after_timestamp = (str)(time.time())
        fdCV.write(before_timestamp+" "+after_timestamp+'\n')
        stub.PublishToCloud(pubYoloreq) 
        

if __name__ == '__main__':
    try:
        main()
    except KeyboardInterrupt:
        fdCV.close()

	
