def fetch(url: str) -> str:
    validate_url_for_ssrf(url, warn_only=False)
    return http_get(url)


settings = Settings(ssrf_protection_enabled=True)
torch_mode = use_deterministic_algorithms(True, warn_only=True)


def validate_url_for_ssrf(url: str, warn_only: bool = False) -> None:
    pass


def http_get(url: str) -> str:
    return ""


def use_deterministic_algorithms(mode, warn_only=False):
    return mode


class Settings:
    def __init__(self, **kw):
        self.kw = kw
