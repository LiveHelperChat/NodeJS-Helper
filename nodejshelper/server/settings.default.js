var settings = {}

/**
 * Should we use publish notifications?
 * Redis have to be running on 127.0.0.1 and 6379 port.
 * Set false to disable
 * */
settings.use_publish_notifications = false;

// If you will be using only web application set this to true, will save some resource
settings.ignore_desktop_client = false;
settings.redis = {};
settings.redis.port = 6379;
settings.redis.host = '127.0.0.1';
settings.redis.options =  {};

settings.web = {};
settings.debug = {};

/**
 * Set path where socket.io is located, it can be also just socket.io
 * */
settings.socketiopath = '/usr/local/lib/node_modules/socket.io';

/**
 * Set your settings
 * */
settings.web.host = "localhost";
settings.web.port = 31129;

/**
 * Enable debug output
 * */
settings.debug.output = true;

module.exports = settings;