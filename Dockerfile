FROM alpine:latest
WORKDIR /root
COPY ./MqttClientTestTool_v1.3.2_Linux64 ./mqttclienttesttool
RUN chmod +x ./mqttclienttesttool
ENTRYPOINT ["./mqttclienttesttool"]
