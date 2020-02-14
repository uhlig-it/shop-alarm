require 'json'

module MQTT
  module Blink1
    class Client
      def initialize(broker:, topic:, logger:)
        @broker = broker
        @topic = topic
        @logger = logger
      end

      def color(red:, green:, blue:)
        @broker.publish(
          @topic,
          { color: { red: red, green: green, blue: blue, } }.to_json
        )
      end
    end
  end
end
