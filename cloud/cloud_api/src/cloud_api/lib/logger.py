import sys

from cloud_api.config import app_config, load_config
from loguru import logger

if not app_config:
    app_config = load_config()

_CONSOLE_FORMAT = (
    "<green>{time:YYYY-MM-DD HH:mm:ss}</green> | "
    "<level>{level: <8}</level> | "
    "<cyan>{name}</cyan>:<cyan>{function}</cyan>:<cyan>{line}</cyan> - "
    "<level>{message}</level> | "
    "<level>{extra}</level>"
)

logger.remove()  # Remove the default logger configuration
logger.add(
    sys.stderr,
    format=_CONSOLE_FORMAT,
    level=app_config.log.level,
    colorize=True,
)
logger.add(
    app_config.log.log_location,
    level=app_config.log.level,
    rotation="10 MB",  # Rotate log file after it reaches 10 MB
    retention="7 days",  # Retain log files for 7 days
    compression="zip",  # Compress rotated log files
    serialize=True,  # Serialize log messages to JSON
)
