require 'blink1'
require_relative '../../../lib/mqtt/blink1/interpreter'

RSpec.describe MQTT::Blink1::Interpreter do
  subject(:parser) { described_class.new(blink1) }
  let(:blink1) { ::Blink1.new.tap { |b1| b1.open } }

  after { blink1.off }

  describe 'a color message' do
    context 'that is valid' do
      let(:message) { <<~EOM
        {
          "color": {
            "red": 255,
            "green": 0,
            "blue": 255
          }
        }
        EOM
      }

      it 'sends the right command' do
        allow(blink1).to receive(:set_rgb)
        parser.interpret(message)
        expect(blink1).to have_received(:set_rgb).with(255, 0, 255)
      end
    end

    context 'that has an additional fade parameter' do
      let(:message) { <<~EOM
        {
          "color": {
            "red": 255,
            "green": 0,
            "blue": 255,
            "fade": 100
          }
        }
        EOM
      }

      it 'sends the right command' do
        allow(blink1).to receive(:fade_to_rgb)
        parser.interpret(message)
        expect(blink1).to have_received(:fade_to_rgb).with(100, 255, 0, 255)
      end
    end

    context 'that has a bogus fade parameter' do
      let(:message) { <<~EOM
        {
          "color": {
            "red": 255,
            "green": 0,
            "blue": 255,
            "fade": "booo"
          }
        }
        EOM
      }

      it 'raises an error' do
        expect {parser.interpret(message)}.to raise_error(MQTT::Blink1::Interpreter::UnexpectedMessageFormat)
      end
    end

    context 'that has a bogus value for green' do
      let(:message) { <<~EOM
        {
          "color": {
            "red": 255,
            "green": "boobar",
            "blue": 255
          }
        }
        EOM
      }

      it 'raises an error' do
        expect {parser.interpret(message)}.to raise_error(MQTT::Blink1::Interpreter::UnexpectedMessageFormat)
      end
    end

    context 'that is missing the value for red' do
      let(:message) { <<~EOM
        {
          "color": {
            "green": 128,
            "blue": 255
          }
        }
        EOM
      }

      it 'raises an error' do
        expect {parser.interpret(message)}.to raise_error(MQTT::Blink1::Interpreter::UnexpectedMessageFormat)
      end
    end
  end
end


__END__

Message format:

color:
  red: 255
  green: 255
  blue: 255
  fade: 100 # optional

pattern:
  red: 255
  green: 255
  blue: 255
  fade: 100
  pos: 0

blink:
  red: 255
  green: 255
  blue: 255
  times: 25

random:
  count: 25 # optional

on

off

play

stop
