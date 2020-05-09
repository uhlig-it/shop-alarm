require 'uri'
require 'http'

module Motion
  #
  # Controls detection of a Motion instance via [web control](https://motion-project.github.io/motion_config.html#webcontrol_interface)
  #
  class WebController
    def initialize(url: 'http://localhost:8080', camera: 0)
      @url = url
      @camera_id = camera
    end

    def start
      get "/#{@camera_id}/detection/start"
      nil
    end

    def pause
      get "/#{@camera_id}/detection/pause"
      nil
    end

    def status
      get("/#{@camera_id}/detection/status").split.last
    end

    def active?
      status == "ACTIVE"
    end

    def toggle
      if active?
        pause
      else
        start
      end
    end

    private

    def get(path)
      url = URI(@url)
      url.path = path
      response = HTTP.timeout(connect: 2, write: 2, read: 2).get(url)
      raise "Unexpected response code: #{response.code}" unless response.code == 200
      response.body.to_s
    end
  end
end
