<?php

#[\AllowDynamicProperties]
class erLhcoreClassExtensionNodejshelper
{
    public function __construct()
    {

    }

    public function run()
    {

        $this->registerAutoload();

        $dispatcher = erLhcoreClassChatEventDispatcher::getInstance();
        $dispatcher->listen('onlineuser.proactive_send_invitation', array($this, 'proactiveInvitationSend'));
        $dispatcher->listen('onlineuser.update_js_vars', array($this, 'proactiveInvitationSend'));

        foreach ([
                     'chat.messages_added_passive',
                     'chat.addmsguser',
                     'chat.visitor_regular_closed',
                     'chat.explicitly_closed',
                     'chat.data_changed_chat',
                     'chat.screenshot_ready',
                     'chat.chatwidgetchat'
                 ] as $event) {
            $dispatcher->listen($event, array($this, 'messageReceived'));
        }

        $dispatcher->listen('chat.stream_flow', array($this, 'streamFlow'));

        foreach (['chat.web_add_msg_admin', 'chat.added_operation'] as $event) {
            $dispatcher->listen($event, array($this, 'messageReceivedAdmin'));
        }

        $dispatcher->listen('chat.bot.alert_icon', array($this, 'chatAttributeUpdate'));

        foreach (['chat.message_updated', 'chat.reaction_visitor', 'chat.reaction_operator', 'chat.msg_removed'] as $event) {
            $dispatcher->listen($event, array($this, 'messageUpdated'));
        }

        // Chat was accepted.
        foreach (['chat.accept', 'chat.update_main_attr', 'chat.genericbot_chat_command_transfer'] as $event) {
            $dispatcher->listen($event, array($this, 'statusChange'));
        }

        $dispatcher->listen('chat.close', array($this, 'chatClose'));
        $dispatcher->listen('chat.notice_update', array($this, 'noticeUpdated'));
        $dispatcher->listen('chat.reload_backoffice', array($this, 'reloadBackOffice'));

        // React based widget init calls
        $dispatcher->listen('widgetrestapi.initchat', array($this, 'initChat'));
        $dispatcher->listen('widgetrestapi.settings', array($this, 'initOnlineVisitor'));

        // Visitor has just read messages
        // Inform admin and mark messages as read one.
        $dispatcher->listen('chat.messages_delivered', array($this, 'messagesWasDelivered'));
        $dispatcher->listen('chat.messages_read', array($this, 'messagesWasRead'));
    }

    public function initOnlineVisitor($params)
    {
        if (
            strpos($_SERVER['HTTP_USER_AGENT'], 'MSIE 10.0') === false &&
            strpos($_SERVER['HTTP_USER_AGENT'], 'MSIE 9.0') === false &&
            strpos($_SERVER['HTTP_USER_AGENT'], 'MSIE 8.0') === false &&
            strpos($_SERVER['HTTP_USER_AGENT'], 'MSIE 7.0') === false) {

            if (!isset($params['output']['init_calls'])) {
                $params['output']['init_calls'] = array();
            }

            $date = time();
            $options = array(
                'hostname' => erLhcoreClassModule::getExtensionInstance('erLhcoreClassExtensionNodejshelper')->getSettingVariable('hostname'),
                'path' => erLhcoreClassModule::getExtensionInstance('erLhcoreClassExtensionNodejshelper')->getSettingVariable('path'),
                'port' => erLhcoreClassModule::getExtensionInstance('erLhcoreClassExtensionNodejshelper')->getSettingVariable('port'),
                'secure' => erLhcoreClassModule::getExtensionInstance('erLhcoreClassExtensionNodejshelper')->getSettingVariable('secure'),
                'hash' => sha1($date . 'Visitor' . erConfigClassLhConfig::getInstance()->getSetting('site', 'secrethash')) . '.' . $date,
                'instance_id' => ((erLhcoreClassModule::getExtensionInstance('erLhcoreClassExtensionNodejshelper')->getSettingVariable('automated_hosting')) ? erLhcoreClassInstance::getInstance()->id : 0),
                'track_visitors' => erLhcoreClassModule::getExtensionInstance('erLhcoreClassExtensionNodejshelper')->getSettingVariable('track_visitors')
            );

            $params['output']['init_calls'][] = array(
                'extension' => 'nodeJSChat',
                'params' => $options
            );
        }
    }

    public function initChat($params)
    {

        if (
            strpos($_SERVER['HTTP_USER_AGENT'], 'MSIE 10.0') === false &&
            strpos($_SERVER['HTTP_USER_AGENT'], 'MSIE 9.0') === false &&
            strpos($_SERVER['HTTP_USER_AGENT'], 'MSIE 8.0') === false &&
            strpos($_SERVER['HTTP_USER_AGENT'], 'MSIE 7.0') === false) {

            if (!isset($params['output']['init_calls'])) {
                $params['output']['init_calls'] = array();
            }

            $date = time();
            $options = array(
                'hostname' => erLhcoreClassModule::getExtensionInstance('erLhcoreClassExtensionNodejshelper')->getSettingVariable('hostname'),
                'path' => erLhcoreClassModule::getExtensionInstance('erLhcoreClassExtensionNodejshelper')->getSettingVariable('path'),
                'port' => erLhcoreClassModule::getExtensionInstance('erLhcoreClassExtensionNodejshelper')->getSettingVariable('port'),
                'secure' => erLhcoreClassModule::getExtensionInstance('erLhcoreClassExtensionNodejshelper')->getSettingVariable('secure'),
                'hash' => sha1($date . 'Visitor' . erConfigClassLhConfig::getInstance()->getSetting('site', 'secrethash')) . '.' . $date,
                'instance_id' => ((erLhcoreClassModule::getExtensionInstance('erLhcoreClassExtensionNodejshelper')->getSettingVariable('automated_hosting')) ? erLhcoreClassInstance::getInstance()->id : 0),
                'track_visitors' => erLhcoreClassModule::getExtensionInstance('erLhcoreClassExtensionNodejshelper')->getSettingVariable('track_visitors'),
            );

            $params['output']['init_calls'][] = array(
                'extension' => 'nodeJSChat',
                'params' => $options
            );
        }
    }

    public function __get($var)
    {
        switch ($var) {

            case 'settings':
                $this->settings = include('extension/nodejshelper/settings/settings.ini.php');
                return $this->settings;
                break;

            default:
                ;
                break;
        }
    }

    public function streamFlow($params)
    {
        $this->updateAdminUI($params['chat']->id, array('as_html' => (isset($params['as_html']) && $params['as_html'] === true), 'msg' => str_replace('/', '__SL__', $params['response']['content']), 'op' => 'sflow'));
    }

    public function messageReceivedAdmin($params)
    {
        if (isset($params['ou']) && $params['ou'] instanceof erLhcoreClassModelChatOnlineUser && $params['chat']->user_status == erLhcoreClassModelChat::USER_STATUS_PENDING_REOPEN) {
            erLhcoreClassNodeJSRedis::instance()->publish('uo_' . $params['ou']->vid, 'o:' . json_encode(array('op' => 'check_message')));
        } else {
            $this->updateAdminUI($params['chat']->id, ['op' => 'cmsg']);
        }
    }

    public function chatAttributeUpdate($params)
    {
        $this->updateAdminUI($params['chat']->id, ['op' => 'uchat']);
    }

    public function updateAdminUI($chatId, $operation)
    {
        if (erLhcoreClassModule::getExtensionInstance('erLhcoreClassExtensionNodejshelper')->getSettingVariable('automated_hosting')) {
            erLhcoreClassNodeJSRedis::instance()->publish('chat_' . erLhcoreClassInstance::getInstance()->id . '_' . $chatId, 'o:' . json_encode($operation));
        } else {
            erLhcoreClassNodeJSRedis::instance()->publish('chat_' . $chatId, 'o:' . json_encode($operation));
        }
    }

    public function getSettingVariable($var)
    {

        switch ($var) {

            case 'hostname':
                return $this->settings['public_settings']['hostname'];
                break;

            case 'path':
                return $this->settings['public_settings']['path'];
                break;

            case 'port':
                return $this->settings['public_settings']['port'];
                break;

            case 'secure':
                return $this->settings['public_settings']['secure'];
                break;

            case 'automated_hosting':
                return $this->settings['automated_hosting'];
                break;

            case 'track_visitors':
                return (isset($this->settings['public_settings']['track_visitors'])) ? $this->settings['public_settings']['track_visitors'] : 1;
                break;

            default:
                return null;;
                break;
        }

    }

    public function registerAutoload()
    {
        spl_autoload_register(array(
            $this,
            'autoload'
        ), true, false);
    }

    public function autoload($className)
    {
        $classesArray = array(
            'erLhcoreClassNodeJSRedis' => 'extension/nodejshelper/classes/lhpredis.php'
        );

        if (key_exists($className, $classesArray)) {
            include_once $classesArray[$className];
        }
    }

    public function proactiveInvitationSend($params)
    {
        if (!isset($params['data_changed']) || $params['data_changed'] === true) {
            erLhcoreClassNodeJSRedis::instance()->publish('uo_' . $params['ou']->vid, 'o:' . json_encode(array('op' => 'check_message')));
        }
    }

    public function messageReceived($params)
    {
        if (!isset($params['chat'])) {
            return;
        }

        $this->updateAdminUI($params['chat']->id, ['op' => 'cmsg']);
    }

    public function messageUpdated($params)
    {
        if (!isset($params['chat'])) {
            return;
        }

        if (!isset($params['msg'])) {
            self::messageReceived($params);
            return;
        }

        $this->updateAdminUI($params['chat']->id, ['op' => 'umsg', 'msid' => $params['msg']->id]);
    }

    public function messagesWasRead($params)
    {
        if (!isset($params['chat'])) {
            return;
        }

        $this->updateAdminUI($params['chat']->id, ['cid' => $params['chat']->id, 'op' => 'msgread']);
    }

    public function messagesWasDelivered($params)
    {
        if (!isset($params['chat'])) {
            return;
        }

        $this->updateAdminUI($params['chat']->id, ['cid' => $params['chat']->id, 'op' => 'msgdel']);
    }

    public function statusChange($params)
    {
        $this->updateAdminUI($params['chat']->id, ['op' => 'schange']);
    }

    public function chatClose($params)
    {
        $this->updateAdminUI($params['chat']->id, ['op' => 'cclose', 'nick' => mb_substr($params['chat']->nick, 0, 10)]);
    }

    public function noticeUpdated()
    {
        if (erLhcoreClassModule::getExtensionInstance('erLhcoreClassExtensionNodejshelper')->getSettingVariable('automated_hosting')) {
            erLhcoreClassNodeJSRedis::instance()->publish('ous_' . erLhcoreClassInstance::getInstance()->id, 'o:' . json_encode(array('op' => 'notice_updated')));
        } else {
            erLhcoreClassNodeJSRedis::instance()->publish('ous_0', 'o:' . json_encode(array('op' => 'notice_updated')));
        }
    }

    public function reloadBackOffice()
    {
        if (erLhcoreClassModule::getExtensionInstance('erLhcoreClassExtensionNodejshelper')->getSettingVariable('automated_hosting')) {
            erLhcoreClassNodeJSRedis::instance()->publish('ous_' . erLhcoreClassInstance::getInstance()->id, 'o:' . json_encode(array('op' => 'reload_page')));
        } else {
            erLhcoreClassNodeJSRedis::instance()->publish('ous_0', 'o:' . json_encode(array('op' => 'reload_page')));
        }
    }

}


