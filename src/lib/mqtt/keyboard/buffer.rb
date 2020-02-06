module MQTT
  module Keyboard
    class Buffer
      def initialize(timeout:, logger:)
        @timeout = timeout
        @logger = logger
        reset
      end

      def append(char)
        writing do
          @chars << char
        end
      end

      def chop!
        writing do
          @chars.chop!
        end
      end

      def reset
        @logger.debug(self.class.name) { 'Resetting' }
        @chars = ''
        @last_changed = Time.now
      end

      def empty?
        reading do
          @chars.empty?
        end
      end

      def to_s
        reading do
          @chars.to_s
        end
      end

      private

      #
      # before returning the current content, check if reset is needed because of age
      #
      def reading(&block)
        age = Time.now - @last_changed

        if age > @timeout && !@chars.empty?
          @logger.debug(self.class.name) { "Content is #{age.truncate(1)} s old (max is #{@timeout} s)" }
          reset
        end

        block.call
      end

      # reset if needed and then update the last_changed timestamp
      def writing(&block)
        reading(&block)
      ensure
        @last_changed = Time.now
      end
    end
  end
end
