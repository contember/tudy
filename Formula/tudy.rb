class Tudy < Formula
  desc "AI-powered local development proxy"
  homepage "https://github.com/contember/tudy"
  version "0.7.1"
  license "MIT"

  on_macos do
    on_arm do
      url "https://github.com/contember/tudy/releases/download/v#{version}/tudy-darwin-arm64.tar.gz"
      sha256 "b1e00c5141722479ee1678c46adbf194679b7db84fac8650d3421c8576412949"
    end
    on_intel do
      url "https://github.com/contember/tudy/releases/download/v#{version}/tudy-darwin-amd64.tar.gz"
      sha256 "87747d879d0ee78923636ac8607f48cd4ae275cbe998629f6d0be333fc4f0721"
    end
  end

  on_linux do
    on_arm do
      url "https://github.com/contember/tudy/releases/download/v#{version}/tudy-linux-arm64.tar.gz"
      sha256 "00828117fb66e566199e77e32f5b94d0a5706583096f6a0a5bf878f4561ead26"
    end
    on_intel do
      url "https://github.com/contember/tudy/releases/download/v#{version}/tudy-linux-amd64.tar.gz"
      sha256 "b3583937fc9fbc4b73392905eae4825a98d1b69b535d21d1a112498be30e40d6"
    end
  end

  def install
    bin.install "caddy" => "tudy-bin"
    bin.install "cli" => "tudy"
    (etc/"tudy").mkpath
    (etc/"tudy").install "Caddyfile" unless (etc/"tudy/Caddyfile").exist?
  end

  service do
    run [opt_bin/"tudy", "run", "--config", etc/"tudy/Caddyfile"]
    keep_alive true
    log_path var/"log/tudy.log"
    error_log_path var/"log/tudy.error.log"
    environment_variables XDG_DATA_HOME: HOMEBREW_PREFIX/"share", XDG_CONFIG_HOME: HOMEBREW_PREFIX/"etc"
  end

  def post_install
    (var/"lib/tudy").mkpath
    # Create Caddy data directory for PKI certificates
    (share/"caddy").mkpath
    # Create example env file if it doesn't exist
    env_file = etc/"tudy/env"
    unless env_file.exist?
      env_file.write <<~EOS
        # Add your API key here:
        # LLM_API_KEY=sk-your-key-here
        #
        # Optional settings:
        # LLM_API_URL=https://openrouter.ai/api/v1/chat/completions
        # MODEL=anthropic/claude-haiku-4.5
      EOS
    end
  end

  def caveats
    <<~EOS
      Run the interactive setup to configure API key, trust certificate, and start:
        tudy setup

      Or configure manually:
        echo "LLM_API_KEY=sk-your-key" >> #{etc}/tudy/env
        sudo brew services start tudy

      Logs: #{var}/log/tudy.log
    EOS
  end

  test do
    assert_match version.to_s, shell_output("#{bin}/tudy version")
  end
end
