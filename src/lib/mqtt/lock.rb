require_relative 'router'
require 'json'

module MQTT
  class Lock
    def initialize(broker:, topic:, code:, logger:)
      @broker = broker
      @topic = topic
      @code = code
      @logger = logger
      @state = nil

      @router = MQTT::Router.new(broker: @broker, logger: @logger)

      @router.add_route('werkstatt/keyboard') do |topic, message|
        @logger.debug(self.class.name) { "Received in #{topic}: #{message}" }
        on_keyboard(message)
      end

      @router.add_route('werkstatt/nfc') do |topic, message|
        @logger.debug(self.class.name) { "Received in #{topic}: #{message}" }
        on_nfc(message)
      end
    end

    def start!
      @router.start!
    end

    private

    def publish(message)
      @logger.debug(self.class.name) { "Publishing to #{@topic}: #{message}" }
      @broker.publish(@topic, message)
    end

    #
    # TODO This could be a proper state machine that publishes on state transitions.
    #
    def on_keyboard(chars)
      case chars[0]
      when '+' # attempt to disarm
        publish('disarming')

        if chars[1..] != @code
          # do not change status, but publish the failed attempt to disarm
          publish('disarm-failed')
        else
          @state = 'disarmed'
        end
      when '-' # attempt to arm
        publish('arming')

        if chars[1..] != @code
          # do not change status, but publish the failed attempt to arm
          publish('arm-failed')
        else
          @state = 'armed'
        end
      else # tamper
        @logger.warn(self.class.name) { "Ignoring keyboard input '#{chars}'" }
      end
      publish(@state)
    end

    def on_nfc(message)
      msg = JSON.parse(message)

      if msg.key?('scanned')
        event = msg['scanned']
      else
        @logger.warn(self.class.name) { "Ignoring NFC message because it is not a 'scanned' event: '#{msg}'" }
        return
      end

      case event['tag']
      when 'Werkstatt-Tür Außen'
        @logger.info(self.class.name) { "Disarmed via NFC by '#{event['user-agent']}'" }
        @state = 'disarmed'
      when 'Werkstatt-Tür Innen'
        @logger.info(self.class.name) { "Armed via NFC by '#{event['user-agent']}'" }
        @state = 'armed'
      else
        @logger.warn(self.class.name) { "Ignoring scan of tag '#{event['tag']}'" }
      end

      publish(@state)
    end
  end
end
