require_relative 'router'

module MQTT
  class Blink1
    def initialize(broker:, topic:, device:, logger:)
      @router = MQTT::Router.new(broker: broker, logger: logger)
      @router.add_route('werkstatt/lock') do |_, message|
        logger.debug(self.class.name) { "Received in #{topic}: #{message}" }
        case message
        when 'armed'
          device.set_rgb(128, 0, 0)
          broker.publish(topic, '{"color":{"r":128,"g":0,"b":0}}')
        when 'disarmed'
          device.set_rgb(0, 128, 0)
          broker.publish(topic, '{"color":{"r":0,"g":128,"b":0}}')
        when 'arm-failed'
          device.blink(0, 128, 0, 3)
          broker.publish(topic, '{"blink":{"count":"3","color":{"r":0,"g":128,"b":0}}}')
        when 'disarm-failed'
          device.blink(128, 0, 0, 3)
          broker.publish(topic, '{"blink":{"count":"3","color":{"r":128,"g":0,"b":0}}}')
        end
      end
    end

    def start!
      @router.start!
    end
  end
end
