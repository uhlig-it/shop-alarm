# Disarm

Listens to an input device via `evdev` and, on Enter, publishes the entered text to an MQTT topic. A listener would than, on message, disable the alarm system.

# Installation

```command
$ apt install --yes ruby ruby-dev libevdev-dev
$ sudo gem install evdev
```

# Synopsis

Example:

```command
$ disarm --device DEVICE --topic TOPIC --timeout TIMEOUT --mqtt-host MQTT_HOST
```

The program will start to listen for events on `DEVICE`. When `ENTER` is pressed, any input that was typed within `TIMEOUT` will be published to the `TOPIC` at [`MQTT_HOST`](https://github.com/mqtt/mqtt.github.io/wiki/URI-Scheme).

Example:

```command
$ disarm --device /dev/input/event0 --topic oldpi/keyboard --timeout 3 --mqtt-host mqtts://user:password@example.com
```

Anything typed on `/dev/input/event0` will be published to `oldpi/keyboard`. If there are more than 3 seconds between two consecutive keystrokes, all previous input will be ignored.
