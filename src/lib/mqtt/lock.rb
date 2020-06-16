require_relative 'router'
require 'json'

module MQTT
  # broker: MQTT broker to publish state updates
  # topic: which topic to publish state updates to
  # code: the secret to arm and disarm the lock
  # motion: camera interface
  class Lock
    def initialize(broker:, topic:, code:, logger:, motion:)
      @broker = broker
      @topic = topic
      @code = code
      @motion = motion
      @logger = logger
      @state = nil

      @router = MQTT::Router.new(broker: @broker, logger: @logger)

      @router.add_route('werkstatt/keyboard') do |t, message|
        @logger.debug(self.class.name) { "Received in #{t}: #{message}" }
        on_keyboard(message)
      end

      @router.add_route('werkstatt/nfc') do |t, message|
        @logger.debug(self.class.name) { "Received in #{t}: #{message}" }
        on_nfc(message)
      end

      @router.add_route('werkstatt/pir') do |t, message|
        @logger.debug(self.class.name) { "Received in #{t}: #{message}" }
        on_pir(message)
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
      @logger.info(self.class.name) { "Received keyboard chars '#{chars}'" }

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
      @logger.info(self.class.name) { "Received NFC message '#{message}'" }

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

    # When the PIR sensor reports begin of motion and the lock is armed, Motion is un-paused and can begin recording.
    # When the PIR sensor reports end of motion, Motion is paused regardless of the state.
    def on_pir(message)
      @logger.info(self.class.name) { "Received PIR message '#{message}'" }

      case message
      when 'begin'
        if @state == 'armed'
          @motion.unpause
          @logger.info(self.class.name) { "Unpaused Motion because lock state is '#{@state}'" }
        else
          @logger.info(self.class.name) { "Not unpausing Motion because state is not 'armed' (it actually is #{@state})" }
        end
      when 'end'
          @motion.pause
          @logger.info(self.class.name) { "Paused Motion (lock state is '#{@state}')" }
      else
        @logger.warn(self.class.name) { "Ignored" }
      end
    end
  end
end
