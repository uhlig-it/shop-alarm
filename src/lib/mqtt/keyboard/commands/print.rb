require_relative 'base'

module MQTT
  module Keyboard
    module Commands
      class Print < Base
        def call(buffer)
          if buffer.empty?
            info 'Buffer is empty'
          else
            info "Buffer is #{buffer}"
          end
        end
      end
    end
  end
end
