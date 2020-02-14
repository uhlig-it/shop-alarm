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
      end

      class Append < Base
        def initialize(char, logger:)
          super(logger: logger)
          @char = char
        end

        def call(buffer)
          debug "Appending #{@char} to '#{buffer}'"
          buffer.append(@char)
        end
      end

      class DummySend < Base
        def call(buffer)
          return if buffer.empty?
          self.warn "NOT sending '#{buffer}' anywhere"
          buffer.reset
        end
      end

      class Publish < Base
        def initialize(broker:, topic:, logger:)
          super(logger: logger)
          @broker = broker
          @topic = topic
          @logger = logger
        end

        def call(buffer)
          if buffer.empty?
            debug "NOT publishing because buffer is empty"
            return
          end

          debug "Publishing '#{buffer}' to '#{@topic}'"
          @broker.publish(@topic, buffer)
          debug "Success"

          buffer.reset
        end
      end

      class Print < Base
        def call(buffer)
          if buffer.empty?
            info 'Buffer is empty'
          else
            info "Buffer is #{buffer}"
          end
        end
      end

      class Chop < Base
        def call(buffer)
          chopped = buffer.chop!
          info "Chopped off #{chopped}"
        end
      end
    end
  end
end
