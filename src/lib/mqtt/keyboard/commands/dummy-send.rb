require_relative 'base'

module MQTT
  module Keyboard
    module Commands
      class DummySend < Base
        def call(buffer)
          return if buffer.empty?
          self.warn "NOT sending '#{buffer}' anywhere"
          buffer.reset
        end
      end
    end
  end
end
