module MQTT
  class Alarm
    def initialize(mqtt:, motion:, keyboard_topic:, status_topic:, logger:)
      @mqtt = mqtt
      @keyboard_topic = keyboard_topic
      @status_topic = status_topic
      @motion = motion
      @logger = logger
    end

    def to_s
      @motion.active? ? 'armed' : 'disarmed'
    end

    def start!(code)
      raise 'Code not present' if code.to_s.empty?

      @mqtt.get(@keyboard_topic) do |topic, message|
        @logger.debug(self.class.name) { "Received in #{topic}: #{message}" }

        if message != code
          @logger.error(self.class.name) { "NOT disarming alarm system because '#{message}' does not match secret code '#{code}'" }
          # TODO Send the wrong code to werkstatt/tamper
        else
          @logger.info(self.class.name) { "Toggling alarm system" }
          @motion.toggle
          @logger.info(self.class.name) { "Alarm system is now #{to_s}" }
          @mqtt.publish(@status_topic, to_s)
        end
      rescue HTTP::Error => e
        @logger.error(self.class.name) { e.message }
      end
    end
  end
end
