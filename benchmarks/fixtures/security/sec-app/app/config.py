"""Configuration from the environment."""

import os


def database_url():
    """The database to use (DATABASE_URL)."""
    return os.environ.get("DATABASE_URL", "sqlite:///local.db")
