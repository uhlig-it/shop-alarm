# frozen_string_literal: true

require_relative 'router'
require 'json'

module MQTT
  # broker: MQTT broker to publish state updates
  # command_topic: which topic to accept commands on
  # status_topic: which topic to publish state updates to
  # code: the secret to arm and disarm the lock
  # motion: camera interface
  #
  # TODO There is at least one state machine hidden that would publish on state transitions
  #
  class Lock
    def initialize(broker:, command_topic:, status_topic:, code:, logger:, motion:)
      @broker = broker
      @status_topic = status_topic
      @code = code
      @motion = motion
      @logger = logger
      @state = nil

      @router = MQTT::Router.new(broker: @broker, logger: @logger)

      # TODO Move to MQTT router
      @router.add_route('werkstatt/nfc/status') do |_, t, message|
        @logger.debug(self.class.name) { "Received in #{t}: #{message}" }
        on_nfc(message)
      end

      @router.add_route(command_topic) do |_, t, message|
        @logger.debug(self.class.name) { "Received in #{t}: #{message}" }
        on_command(message)
      end
    end

    def start!
      @router.start!
    end

    private

    def change_state(new_state)
      @logger.debug(self.class.name) { "Changing state from #{@state} to #{new_state}" }
      @state = new_state

      @logger.debug(self.class.name) { "Publishing new state to #{@status_topic}: #{@state}" }
      @broker.publish(@status_topic, @state)

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

    def on_command(message)
      @logger.info(self.class.name) { "Received command '#{message}'" }

      msg = JSON.parse(message)

      if msg['code'] != @code
        @logger.warn(self.class.name) { "Ignoring command because it has no or the wrong code: '#{msg}'" }
        return
      end

      if msg.key?('action')
        action = msg['action']
      else
        @logger.warn(self.class.name) { "Ignoring command because it has no 'action': '#{msg}'" }
        return
      end

      case action
      when 'arm'
        @logger.info(self.class.name) { "Armed via command by '#{msg['user-agent']}'" }
        change_state('armed')
      when 'disarm'
        @logger.info(self.class.name) { "Disarmed via command by '#{msg['user-agent']}'" }
        change_state('disarmed')
      else
        @logger.warn(self.class.name) { "Ignoring action '#{action}'" }
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
  end
end
