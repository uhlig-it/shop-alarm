require_relative '../../lib/mqtt/router'
require 'rspec/eventually'
require 'logger'

RSpec.describe MQTT::Router do
  subject(:router) { described_class.new(mqtt: mqtt, logger: logger) }
  let(:mqtt) { MQTT::Client.new('mqtts://mqtt:q1L5ZRGHMeFmnRlxKvsFY3ACs@mqtt.uhlig.it/werkstatt/blink1') }
  let(:received_messages) { Hash.new }
  let(:logger) { instance_double(Logger) }

  before { mqtt.connect }
  after  { mqtt.disconnect }

  context 'no route' do
    it 'does not receive messages' do
      start_router
      mqtt.publish('test', 'welcome')
      expect(received_messages).to be_empty
    end
  end

  context 'simple topic' do
    before do
      router.add_route('test') do |topic, message|
        received_messages[topic] = message
      end
    end

    it 'receives the message' do
      start_router
      mqtt.publish('test', 'welcome')
      expect { received_messages['test'] }.to eventually eq 'welcome'
    end
  end

  context 'single-level wildcard' do
    before do
      router.add_route('foo/+/bar') do |topic, message|
        received_messages[topic] = message
      end
    end

    it 'receives the message' do
      start_router
      mqtt.publish('foo/some/bar', 'it works!')
      expect { received_messages['foo/some/bar'] }.to eventually eq 'it works!'
    end
  end

  context 'multi-level wildcard'

  private

  def start_router
    Thread.new { router.start! }
    sleep 1 # needs some time to get up
  end
end

__END__

Ideas:
* # is a simple ends_with?('#')
* +: split topic at `+`, and check if it starts with the first part and ends with the second.
