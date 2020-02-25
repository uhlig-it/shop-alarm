module MQTT
  module Keyboard
    module Commands
      class Base
        def initialize(logger:)
          @logger = logger
        end

        def to_s
          self.class.name
        end

        def debug(message)
          @logger.debug(self) { message }
        end

        def info(message)
          @logger.info(self) { message }
        end

        def warn(message)
          @logger.warn(self) { message }
        end

        def error(message)
          @logger.error(self) { message }
        end
      end
    end
  end
end
