<?php

return array(
    'connect_db' => 'localhost',
    'connect_db_id' => 0,
    'automated_hosting' => false,
    'public_settings' => array(
        'hostname' => (isset($_SERVER['HTTP_HOST']) ? explode(':',$_SERVER['HTTP_HOST'])[0] : null),
        'path' => '/socketcluster/',
        'port' => null, //some custom port
        'secure' => null, // true || false
        'track_visitors' => 1 // true || false
    )
);

?>