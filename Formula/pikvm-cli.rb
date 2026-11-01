# Source installation is available before the first binary release is promoted.
class PikvmCli < Formula
  desc "Control PiKVM from terminals and automation"
  homepage "https://github.com/infoe-labs/pikvm-cli"
  head "https://github.com/infoe-labs/pikvm-cli.git", branch: "main"

  depends_on "go" => :build

  def install
    system "go", "build", *std_go_args(ldflags: "-s -w -X main.version=HEAD"), "./cmd/pikvm-cli"
  end

  test do
    assert_match "Control PiKVM", shell_output("#{bin}/pikvm-cli --help")
  end
end
