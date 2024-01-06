# Shop Alarm

A workshop security system based on events published to MQTT.

# Components

## `MQTT::Blink1` actor

When `armed`, `disarmed`, `arm-failed` or `disarm-failed` events appear on `werkstatt/lock`, it sets the color of the _local_ `Blink1` device accordingly.

In addition, it publishes a `color`, `fade` etc. event to `werkstatt/blink1`. Other components may be interested.

## `MQTT::NFC` sensor

Publishes a `scanned` event to `werkstatt/nfc` as it is called via SSH. It passes the NFC tag's ID as well as the calling user agent (e.g. `iPhoneSteffen`).

## `MQTT::Telegram` actor

Subscribes to `werkstatt/telegram`. Upon `message` events, sends the payload of the event to Telegram.

# FAQ

Q: What if we want to combine two events, or have a dependency? E.g. if the NFC event fires, but the lock is disarmed, the alarm event should not be published?
A: We need to find a way to query the status of a sensor. Perhaps we need a (compound. virtual) processor (the "Alarm System") that, upon one or more events, evaluates the state of multiple sensors in order to take some action (e.g. send a Telegram message or emit an `alarm` event).

Q: What if another component wants to change the color of the Blink1 device?
A: It would have to:

   1. publish a domain event about the thing that just happened, and
   1. make a change to the code of the `MQTT::Blink1` so that it understands the new event.

   The point is that one component cannot just change the state of another one. Both must agree on the existence of the new event, and it is up to `MQTT::Blink1` to make something happen when the new event appears.

# Deployment

```bash
$ cd deployment
$ ansible-playbook playbook.yml
```

Ansible will deploy the service, enable and start it.

# TODO

* Metrics, e.g. the lock state and when motion detected some, well, motion
  => some are useful as numeric data (e.g. how long an alert went on)
  => some are useful as events that can be show in graphs (e.g. motion detected, alarm disarmed, etc.) => https://gist.github.com/suhlig/049b068185f216824d33756dae99172a
* The Blink1 approach is wrong. We should be talking to it only via MQTT, and send it the rgb/on/off messages etc:

  ```ruby
  broker.publish(@topic, %Q[{"color":{"r":#{r},"g":#{g},"b":#{b}}}])
  broker.publish(@topic, %Q[{"blink":{"count":#{count},"color":{"r":#{r},"g":#{g},"b":#{b}}}}])
  ```

  Then, one more function simply becomes a matter of translating from one MQTT message to another one:

  ```
  werkstatt/lock: armed => werkstatt/blink1: {"color":{"r":255,"g":0,"b":0}}
  ```

* Distinguish between commands and status updates (Tasmota uses `cmnd/foo` and `status/foo`)
* On first start, `motion` needs to be restarted; otherwise the camera will be almost black
* Some topics are still hardcoded (`werkstatt/lock`)
* Delay arming a couple of seconds after it being enabled, so we don't raise an alert on our way out
* Tests?
* Add an SSD1306 display, just for kicks
