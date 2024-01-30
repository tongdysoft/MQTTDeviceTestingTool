FROM alpine:latest
WORKDIR /root
COPY ./MqttClientTestTool_v1.5.5_Linux64 ./mqttclienttesttool
RUN chmod +x ./mqttclienttesttool
ENTRYPOINT ["./mqttclienttesttool"]
