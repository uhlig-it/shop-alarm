require 'blink1'
require_relative '../../../lib/mqtt/blink1/interpreter'

RSpec.describe MQTT::Blink1::Interpreter do
  subject(:parser) { described_class.new(blink1) }
  let(:blink1) { ::Blink1.new.tap { |b1| b1.open } }

  after { blink1.off }

  describe 'an unknown message' do
    let(:message) { <<~EOM
      {
        "foo": "bar"
      }
      EOM
    }

    it 'raises an error' do
      expect {parser.interpret(message)}.to raise_error(MQTT::Blink1::UnrecognizedCommand)
    end
  end

  describe 'a "color" message' do
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
        expect {parser.interpret(message)}.to raise_error(MQTT::Blink1::UnexpectedMessageFormat)
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
        expect {parser.interpret(message)}.to raise_error(MQTT::Blink1::UnexpectedMessageFormat)
      end
    end
  end

  describe 'a "blink" message' do
    context 'that is valid' do
      let(:message) { <<~EOM
        {
          "blink": {
            "count": 25,
            "color": {
              "red": 128,
              "green": 64,
              "blue": 11
            }
          }
        }
        EOM
      }

      it 'sends the right command' do
        allow(blink1).to receive(:blink)
        parser.interpret(message)
        expect(blink1).to have_received(:blink).with(128, 64, 11, 25)
      end
    end

    context 'that has a bogus value for count' do
      let(:message) { <<~EOM
        {
          "blink": {
            "count": "hocus",
            "color": {
              "red": 255,
              "green": 255,
              "blue": 255
            }
          }
        }
        EOM
      }

      it 'raises an error' do
        expect {parser.interpret(message)}.to raise_error(MQTT::Blink1::UnexpectedMessageFormat)
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
        expect {parser.interpret(message)}.to raise_error(MQTT::Blink1::UnexpectedMessageFormat)
      end
    end
  end

  describe 'a "fade" message' do
    let(:message) { <<~EOM
      {
        "fade": {
          "time": 100,
          "color": {
            "red": 255,
            "green": 0,
            "blue": 255
          }
        }
      }
      EOM
    }

    it 'sends the right command' do
      allow(blink1).to receive(:fade_to_rgb)
      parser.interpret(message)
      expect(blink1).to have_received(:fade_to_rgb).with(100, 255, 0, 255)
    end

    context 'that has a bogus time parameter' do
      let(:message) { <<~EOM
        {
          "fade": {
            "time": "boo",
            "color": {
              "red": 255,
              "green": 0,
              "blue": 255
            }
          }
        }
        EOM
      }

      it 'raises an error' do
        expect {parser.interpret(message)}.to raise_error(MQTT::Blink1::UnexpectedMessageFormat)
      end
    end
  end

  describe 'an "on" message' do
    let(:message) { '"on"' }

    it 'sends the right command' do
      allow(blink1).to receive(:on)
      parser.interpret(message)
      expect(blink1).to have_received(:on)
    end
  end

  describe 'an "off" message' do
    let(:message) { '"off"' }

    it 'sends the right command' do
      allow(blink1).to receive(:off)
      parser.interpret(message)
      expect(blink1).to have_received(:off)
    end
  end
end


__END__

TODO

random:
  count: 25 # optional

pattern:
  pos: 0
  fade:
    time: 100 # optional
    color:
      red: 255
      green: 255
      blue: 255

play

stop
