import Ember from 'ember';
import Block from "../models/block";
import config from '../config/environment';

export default Ember.Route.extend({
  auth: Ember.inject.service('auth'),

  actions: {
    error: function(reason, transition) {
      this.transitionTo('/login');
      console.log(reason + transition);
      return false;
    }
  },

	model: function() {
    var url = config.APP.ApiUrl + 'api/blocks';
    return Ember.$.ajax({
      method: 'get',
      url: url,
      crossDomain: true,
      xhrFields: {
        withCredentials: true
      },
      //headers: {"Ction": "close"},
    }).then(function(data) {
			if (data.candidates) {
				data.candidates = data.candidates.map(function(b) {
					return Block.create(b);
				});
			}
			if (data.immature) {
				data.immature = data.immature.map(function(b) {
					return Block.create(b);
				});
			}
			if (data.matured) {
				data.matured = data.matured.map(function(b) {
					return Block.create(b);
				});
			}
			return data;
    });
	},

  beforeModel() {
    if(!this.get('auth').isLoggedIn()) {
        this.transitionTo('login');
    }
  },

  setupController: function(controller, model) {
    this._super(controller, model);
    Ember.run.later(this, this.refresh, 5000);
  }
});
