require_relative 'router'
require 'json'

module MQTT
  # broker: MQTT broker to publish state updates
  # topic: which topic to publish state updates to
  # code: the secret to arm and disarm the lock
  # motion: camera interface
  #
  # TODO There is at least one state machine hidden that would publish on state transitions
  #
  class Lock
    def initialize(broker:, topic:, code:, logger:, motion:)
      @broker = broker
      @topic = topic
      @code = code
      @motion = motion
      @logger = logger
      @state = nil

      @router = MQTT::Router.new(broker: @broker, logger: @logger)

      @router.add_route('werkstatt/keyboard') do |_, t, message|
        @logger.debug(self.class.name) { "Received in #{t}: #{message}" }
        on_keyboard(message)
      end

      @router.add_route('werkstatt/nfc') do |_, t, message|
        @logger.debug(self.class.name) { "Received in #{t}: #{message}" }
        on_nfc(message)
      end

      @router.add_route('werkstatt/pir') do |_, t, message|
        @logger.debug(self.class.name) { "Received in #{t}: #{message}" }
        on_pir(message)
      end
    end

    def start!
      @router.start!
    end

    private

    def change_state(new_state)
      @logger.debug(self.class.name) { "Changing state from #{@state} to #{new_state}" }
      @state = new_state

      @logger.debug(self.class.name) { "Publishing new state to #{@topic}: #{@state}" }
      @broker.publish(@topic, @state)

      case @state
        when 'armed'
          @logger.debug(self.class.name) { "Starting Motion from previous #{@motion.status}" }
          @motion.start
        when 'disarmed'
          @logger.debug(self.class.name) { "Stopping Motion from previous #{@motion.status}" }
          @motion.stop
        else
          @logger.debug(self.class.name) { "Keeping Motion at #{@motion.status}" }
      end
    end

    def on_keyboard(chars)
      @logger.info(self.class.name) { "Received keyboard chars '#{chars}'" }

      case chars[0]
      when '+' # attempt to disarm
        change_state('disarming')

        if chars[1..] != @code
          change_state('disarm-failed')
        else
          change_state('disarmed')
        end
      when '-' # attempt to arm
        change_state('arming')

        if chars[1..] != @code
          change_state('arm-failed')
        else
          change_state('armed')
        end
      else # tamper
        @logger.warn(self.class.name) { "Ignoring keyboard input '#{chars}'" }
      end
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
        change_state('disarmed')
      when 'Werkstatt-Tür Innen'
        @logger.info(self.class.name) { "Armed via NFC by '#{event['user-agent']}'" }
        change_state('armed')
      else
        @logger.warn(self.class.name) { "Ignoring scan of tag '#{event['tag']}'" }
      end
    end

    # When the PIR sensor reports begin of motion and the lock is armed, Motion is started and can begin recording if it detects motion.
    # When the PIR sensor reports end of motion, Motion is stopped regardless of the state.
    def on_pir(message)
      @logger.info(self.class.name) { "Received PIR message '#{message}'" }

      case message
      when 'begin'
        if @state == 'armed'
          @logger.info(self.class.name) { "Starting Motion because lock state is '#{@state}'" }
          @motion.start
        else
          @logger.info(self.class.name) { "Not starting Motion because state is not 'armed' (it actually is #{@state})" }
        end
      when 'end'
        @logger.info(self.class.name) { "Stopping Motion (lock state is '#{@state}')" }
        @motion.stop
      else
        @logger.warn(self.class.name) { "Ignored" }
      end
    end
  end
end
