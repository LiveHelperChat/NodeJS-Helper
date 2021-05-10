<?php

erLhcoreClassRestAPIHandler::setHeaders();

if (erLhcoreClassUser::instance()->isLogged() && erLhcoreClassUser::instance()->hasAccessTo('lhchat', 'use')) {
    $date = time();
    echo json_encode([
        'instance_id' => (erLhcoreClassModule::getExtensionInstance('erLhcoreClassExtensionNodejshelper')->getSettingVariable('automated_hosting') ? erLhcoreClassInstance::getInstance()->id : 0),
        'hash' => sha1($date . 'Operator' . erConfigClassLhConfig::getInstance()->getSetting('site','secrethash')) . '.' . $date
    ]);
} else {
    echo json_encode("invalid");
}

exit;

?>