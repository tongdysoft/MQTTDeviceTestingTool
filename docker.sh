unzip MqttClientTestTool_v1.5.1_Linux64.zip
docker stop mqttclienttesttool
docker rm mqttclienttesttool
docker rmi mqttclienttesttool
docker build -t mqttclienttesttool .
docker run -it -p 1883:1883 --name mqttclienttesttool -d mqttclienttesttool
