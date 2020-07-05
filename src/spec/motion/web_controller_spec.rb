require_relative '../../lib/motion/web_controller'

RSpec.describe Motion::WebController do
  subject(:shop) { described_class.new(url: url, camera: camera_id) }
  let(:url) { 'http://shop.uhlig.it:8080' }
  let(:camera_id) { 0 }

  it 'exists' do
    expect(subject).to be
  end

  context 'the request times out' do
    let(:url) { 'http://example.com:8080' }

    it 'raises an error' do
      expect { subject.status }.to raise_error(HTTP::TimeoutError)
    end
  end

  context 'when the service is active' do
    before do
      subject.start
    end

    it 'reports status ACTIVE' do
      expect(subject.status).to eq('ACTIVE')
    end

    it 'returns true for #active?' do
      expect(subject.active?).to be_truthy
    end

    context 'and it is started again' do
      before do
        subject.start
      end

      it 'is still active' do
        expect(subject.active?).to be_truthy
      end
    end

    context 'and it is toggled' do
      before do
        subject.toggle
      end

      it 'is stopped' do
        expect(subject.active?).to be_falsey
      end
    end
  end

  context 'when the service is stopped' do
    before do
      subject.stop
    end

    it 'reports status PAUSE' do
      expect(subject.status).to eq('PAUSE')
    end

    it 'returns false for #active?' do
      expect(subject.active?).to be_falsey
    end

    context 'and it is stopped again' do
      before do
        subject.stop
      end

      it 'is still inactive' do
        expect(subject.active?).to be_falsey
      end
    end

    context 'and it is toggled' do
      before do
        subject.toggle
      end

      it 'is active' do
        expect(subject.active?).to be_truthy
      end
    end
  end
end
