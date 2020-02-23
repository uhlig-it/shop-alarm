require_relative 'base'

module MQTT
  module Keyboard
    module Commands
      class Chop < Base
        def call(buffer)
          chopped = buffer.chop!
          info "Chopped off #{chopped}"
        end
      end
    end
  end
end
