def fetch(url: str) -> str:
    validate_url_for_ssrf(url, warn_only=True)
    return http_get(url)


settings = Settings(ssrf_protection_enabled=False)


def validate_url_for_ssrf(url: str, warn_only: bool = False) -> None:
    pass


def http_get(url: str) -> str:
    return ""


class Settings:
    def __init__(self, **kw):
        self.kw = kw
