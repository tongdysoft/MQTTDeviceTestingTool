docker stop MQTTDeviceTestingTool
docker rm MQTTDeviceTestingTool
docker rmi MQTTDeviceTestingTool
docker build -t MQTTDeviceTestingTool .
docker run -it -p 1883:1883 --name MQTTDeviceTestingTool -d MQTTDeviceTestingTool
