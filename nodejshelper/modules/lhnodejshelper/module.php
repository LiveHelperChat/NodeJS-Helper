<?php

$Module = array( "name" => "NodeJSHelper",
    'variable_params' => true );

$ViewList = array();

$ViewList['tokenvisitor'] = array(
    'params' => array('chat_id','hash'),
    'uparams' => array(),
);

$ViewList['tokenadmin'] = array(
    'params' => array(),
    'uparams' => array()
);