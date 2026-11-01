class PikvmCli < Formula
  desc "Control PiKVM from terminals and automation"
  homepage "https://github.com/infoe-labs/pikvm-cli"
  version "0.1.0"

  on_macos do
    if Hardware::CPU.arm?
      url "https://github.com/infoe-labs/pikvm-cli/releases/download/v0.1.0/pikvm-cli_v0.1.0_darwin_arm64.tar.gz"
      sha256 "482b62009d0d6b95467a9e21462945771da4e2c7c1462f05353a92971ab145a5"
    else
      url "https://github.com/infoe-labs/pikvm-cli/releases/download/v0.1.0/pikvm-cli_v0.1.0_darwin_amd64.tar.gz"
      sha256 "3551b3d15d4d3cab72aa4dd131767313bc7dd6c6b988657dcc29c8db38cdf499"
    end
  end

  on_linux do
    if Hardware::CPU.arm?
      url "https://github.com/infoe-labs/pikvm-cli/releases/download/v0.1.0/pikvm-cli_v0.1.0_linux_arm64.tar.gz"
      sha256 "130979b5281e6fba54429804921939ec26e6448f6292f7f0417393a3ce7b003f"
    else
      url "https://github.com/infoe-labs/pikvm-cli/releases/download/v0.1.0/pikvm-cli_v0.1.0_linux_amd64.tar.gz"
      sha256 "f3105eda521fc795c698b3782d574bfecc40b7030e4ec2e1f84d31e775b09260"
    end
  end

  def install
    bin.install "pikvm-cli"
  end

  test do
    assert_match version.to_s, shell_output("#{bin}/pikvm-cli --version")
  end
end
