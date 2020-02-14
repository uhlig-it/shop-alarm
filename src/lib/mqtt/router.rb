require 'mqtt'

module MQTT
  class Router
    NoRouteDefined = Class.new(StandardError) do
      def initialize(topic)
        super("Subscribed to, but no route defined for #{topic}")
      end
    end

    def initialize(broker:, logger:)
      @broker = broker
      @logger = logger
      @routes = Hash.new
    end

    def add_route(topic, &block)
      @routes[topic] = block
    end

    def start!
      @logger.warn(self.class.name) { 'Nothing to subscribe to' } if topics.empty?

      @mqtt.get(*topics) do |topic, message|
        raise NoRouteDefined.new(topic) unless @routes.has_key?(topic)
        @routes.fetch(topic).call(topic, message)
      end
    end

    def topics
      @routes.keys
    end
  end
end
