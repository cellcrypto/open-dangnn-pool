import Ember from 'ember';
import config from '../config/environment';

export default Ember.Route.extend({
  intl: Ember.inject.service(),
  //ajax: Ember.inject.service('ajax'),
  auth: Ember.inject.service('auth'),

  actions: {
    error: function(reason, transition) {
      this.transitionTo('/login');
      console.log("appl login err:" + reason + "trans:" + transition);
      return false;
    },
    loginform: function() {
      this.transitionTo('/login');
      return ;
    }
  },

  beforeModel() {
    this.get('intl').setLocale('en-us');
  },

	model: function() {

    if (this.get('auth').isLoggedIn() === false) {
      this.transitionTo('/login');
      return ;
    }

    var token = this.get('auth').getIdToken();
    var url = config.APP.ApiUrl + 'api/stats';

    return Ember.$.ajax({
      method: 'get',
      url: url,
      crossDomain: true,
      xhrFields: {
        withCredentials: true
      },
      //headers: {"Ction": "close"},
    }).then((result) => {
      return Ember.Object.create(result);
    }, (err) => {
      if (err.status === 401) {
        this.get('auth').clearIdToken();
        this.transitionTo('/login');
        alert('login id and password');
      } else {
        alert('err: ' + err.responseText);
      }
    });
	},

  setupController: function(controller, model) {
    this._super(controller, model);
    controller.set('loggedIn', this.get('auth').isLoggedIn());
    Ember.run.later(this, this.refresh, 5000);
  }
});
