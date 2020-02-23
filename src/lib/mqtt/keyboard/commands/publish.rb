require_relative 'base'

module MQTT
  module Keyboard
    module Commands
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
    end
  end
end
