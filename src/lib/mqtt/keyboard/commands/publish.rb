require_relative 'base'

module MQTT
  module Keyboard
    module Commands
      class Publish < Base
        def initialize(url:, topic:, logger:)
          super(logger: logger)
          @url = url
          @topic = topic
          @logger = logger
        end

        def call(buffer)
          if buffer.empty?
            debug "NOT publishing because buffer is empty"
            return
          end

          MQTT::Client.connect(@url) do |broker|
            debug "Publishing '#{buffer}' to '#{@topic}'"
            broker.publish(@topic, buffer)
            debug "Success"
          rescue => e
            error(e)
          end

          buffer.reset
        end
      end
    end
  end
end
